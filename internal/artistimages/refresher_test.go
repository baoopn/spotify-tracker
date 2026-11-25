package artistimages

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/spotify"
)

type stored struct {
	url    string
	genres []string
}

type fakeStore struct {
	ids []string
	set map[string]stored
}

func (f *fakeStore) ArtistsNeedingProfile(context.Context, time.Time, int) ([]string, error) {
	return f.ids, nil
}

func (f *fakeStore) SetArtistProfile(_ context.Context, id, url string, genres []string) error {
	f.set[id] = stored{url, genres}
	return nil
}

type fakeFetcher map[string]error

func (f fakeFetcher) ArtistProfile(_ context.Context, id string) (spotify.ArtistProfile, error) {
	if err := f[id]; err != nil {
		return spotify.ArtistProfile{}, err
	}
	switch id {
	case "noimg":
		return spotify.ArtistProfile{Found: true, Genres: []string{}}, nil
	case "nilgenres":
		return spotify.ArtistProfile{Found: true}, nil
	case "missing":
		return spotify.ArtistProfile{}, nil
	}
	return spotify.ArtistProfile{Found: true, ImageURL: "https://i.scdn.co/image/" + id,
		Genres: []string{"pop", id}}, nil
}

func newTest(st *fakeStore, f Fetcher) *Refresher {
	r := New(st, f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.sleep = func(context.Context, time.Duration) bool { return true }
	return r
}

func TestRunOnceStoresProfiles(t *testing.T) {
	st := &fakeStore{ids: []string{"a1", "noimg", "nilgenres", "missing"}, set: map[string]stored{}}
	if pause := newTest(st, fakeFetcher{}).RunOnce(context.Background()); pause != 0 {
		t.Fatalf("pause = %v", pause)
	}
	want := map[string]stored{
		"a1":        {"https://i.scdn.co/image/a1", []string{"pop", "a1"}},
		"noimg":     {"", []string{}},
		"nilgenres": {"", []string{}},
		"missing":   {"", nil},
	}
	for id, w := range want {
		got, ok := st.set[id]
		if !ok || got.url != w.url || !reflect.DeepEqual(got.genres, w.genres) || (got.genres == nil) != (w.genres == nil) {
			t.Errorf("%s: got %+v (recorded %v, nil genres %v), want %+v", id, got, ok, got.genres == nil, w)
		}
	}
}

func TestRunOnceBacksOffOnRateLimit(t *testing.T) {
	st := &fakeStore{ids: []string{"a1", "limited", "a2"}, set: map[string]stored{}}
	f := fakeFetcher{"limited": &spotify.RateLimitError{RetryAfter: 90 * time.Second}}
	if pause := newTest(st, f).RunOnce(context.Background()); pause != 90*time.Second {
		t.Fatalf("pause = %v, want 90s", pause)
	}
	if _, ok := st.set["a2"]; ok {
		t.Error("kept fetching after a rate limit")
	}
	if _, ok := st.set["limited"]; ok {
		t.Error("recorded a rate-limited lookup as done")
	}
}

func TestRunOnceStopsOnAuthError(t *testing.T) {
	for _, err := range []error{spotify.ErrNotConnected, spotify.ErrInvalidGrant} {
		st := &fakeStore{ids: []string{"a1", "a2"}, set: map[string]stored{}}
		newTest(st, fakeFetcher{"a1": err}).RunOnce(context.Background())
		if len(st.set) != 0 {
			t.Errorf("%v: recorded lookups: %v", err, st.set)
		}
	}
}

// A permanent 4xx for one artist is recorded without data and the batch goes
// on; a 5xx is not recorded (retried next batch) and the batch goes on too.
func TestRunOnceSkipsPermanentClientError(t *testing.T) {
	st := &fakeStore{ids: []string{"bad", "a1"}, set: map[string]stored{}}
	f := fakeFetcher{"bad": &spotify.StatusError{Code: 400}}
	newTest(st, f).RunOnce(context.Background())
	if got, ok := st.set["bad"]; !ok || got.url != "" || got.genres != nil {
		t.Errorf("bad: got %+v (recorded %v), want recorded with nil genres", got, ok)
	}
	if got := st.set["a1"]; got.url != "https://i.scdn.co/image/a1" {
		t.Errorf("a1 not fetched after a 400: %+v", got)
	}

	st = &fakeStore{ids: []string{"down", "a1"}, set: map[string]stored{}}
	f = fakeFetcher{"down": &spotify.StatusError{Code: 503}}
	newTest(st, f).RunOnce(context.Background())
	if _, ok := st.set["down"]; ok {
		t.Error("recorded a lookup that failed with a 5xx")
	}
	if _, ok := st.set["a1"]; !ok {
		t.Error("a1 not fetched after a 5xx")
	}
}

// Three transient failures in a row end the batch.
func TestRunOnceStopsAfterRepeatedTransientErrors(t *testing.T) {
	down := errors.New("network down")
	st := &fakeStore{ids: []string{"d1", "d2", "a1", "d3", "d4", "d5", "a2"}, set: map[string]stored{}}
	newTest(st, fakeFetcher{"d1": down, "d2": down, "d3": down, "d4": down, "d5": down}).RunOnce(context.Background())
	if _, ok := st.set["a1"]; !ok {
		t.Error("a1 not fetched: success did not reset the failure count")
	}
	if _, ok := st.set["a2"]; ok {
		t.Error("kept fetching after three failures in a row")
	}
}

// Wake cuts the wait between batches short, never blocks, and does not
// shorten a rate-limit pause (Run passes a nil wake channel then).
func TestRefresherWakeCutsWait(t *testing.T) {
	r := New(nil, nil, nil)
	r.Wake()
	r.Wake() // buffered once: must not block
	done := make(chan bool, 1)
	go func() { done <- r.waitWake(context.Background(), time.Hour, r.wake) }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("waitWake returned false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wake did not cut the wait short")
	}
	r.Wake()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if r.waitWake(ctx, time.Hour, nil) {
		t.Fatal("a nil wake channel must not end the pause")
	}
}
