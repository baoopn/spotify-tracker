package artistimages

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/spotify"
)

type fakeStore struct {
	ids []string
	set map[string]string
}

func (f *fakeStore) ArtistsNeedingImage(context.Context, time.Time, int) ([]string, error) {
	return f.ids, nil
}

func (f *fakeStore) SetArtistImage(_ context.Context, id, url string) error {
	f.set[id] = url
	return nil
}

type fakeFetcher map[string]error

func (f fakeFetcher) ArtistImage(_ context.Context, id string) (string, error) {
	if err := f[id]; err != nil {
		return "", err
	}
	if id == "noimg" {
		return "", nil
	}
	return "https://i.scdn.co/image/" + id, nil
}

func newTest(st *fakeStore, f Fetcher) *Refresher {
	r := New(st, f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.sleep = func(context.Context, time.Duration) bool { return true }
	return r
}

func TestRunOnceStoresImagesAndLookups(t *testing.T) {
	st := &fakeStore{ids: []string{"a1", "noimg", "a2"}, set: map[string]string{}}
	if pause := newTest(st, fakeFetcher{}).RunOnce(context.Background()); pause != 0 {
		t.Fatalf("pause = %v", pause)
	}
	want := map[string]string{"a1": "https://i.scdn.co/image/a1", "noimg": "", "a2": "https://i.scdn.co/image/a2"}
	for id, url := range want {
		if got, ok := st.set[id]; !ok || got != url {
			t.Errorf("%s: got %q (recorded %v), want %q", id, got, ok, url)
		}
	}
}

func TestRunOnceBacksOffOnRateLimit(t *testing.T) {
	st := &fakeStore{ids: []string{"a1", "limited", "a2"}, set: map[string]string{}}
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
	st := &fakeStore{ids: []string{"a1", "a2"}, set: map[string]string{}}
	f := fakeFetcher{"a1": errors.New("spotify: not authorized")}
	newTest(st, f).RunOnce(context.Background())
	if len(st.set) != 0 {
		t.Errorf("recorded lookups after an auth error: %v", st.set)
	}
}

// A permanent 4xx for one artist is recorded without an image and the batch
// goes on; a 5xx still stops the batch for a retry.
func TestRunOnceSkipsPermanentClientError(t *testing.T) {
	st := &fakeStore{ids: []string{"bad", "a1"}, set: map[string]string{}}
	f := fakeFetcher{"bad": &spotify.StatusError{Code: 400}}
	newTest(st, f).RunOnce(context.Background())
	if got, ok := st.set["bad"]; !ok || got != "" {
		t.Errorf("bad: got %q (recorded %v), want recorded empty", got, ok)
	}
	if got := st.set["a1"]; got != "https://i.scdn.co/image/a1" {
		t.Errorf("a1 not fetched after a 400: %q", got)
	}

	st = &fakeStore{ids: []string{"down", "a1"}, set: map[string]string{}}
	f = fakeFetcher{"down": &spotify.StatusError{Code: 503}}
	newTest(st, f).RunOnce(context.Background())
	if len(st.set) != 0 {
		t.Errorf("recorded lookups after a 5xx: %v", st.set)
	}
}
