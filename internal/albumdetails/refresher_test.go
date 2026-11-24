package albumdetails

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

type fakeStore struct {
	ids      []string
	set      map[string]*domain.AlbumDetails
	deferred map[string]time.Duration
}

func (f *fakeStore) AlbumsNeedingDetails(context.Context, time.Time, int) ([]string, error) {
	return f.ids, nil
}

func (f *fakeStore) SetAlbumDetails(_ context.Context, id string, d *domain.AlbumDetails) error {
	f.set[id] = d
	return nil
}

func (f *fakeStore) DeferAlbumDetails(_ context.Context, id string, age time.Duration) error {
	f.deferred[id] = age
	return nil
}

type fakeFetcher map[string]error

func (f fakeFetcher) AlbumDetails(_ context.Context, id string) (*domain.AlbumDetails, error) {
	if err := f[id]; err != nil {
		return nil, err
	}
	if id == "missing" {
		return nil, nil
	}
	return &domain.AlbumDetails{AlbumType: "album", UPC: "upc-" + id, Copyrights: []domain.Copyright{}}, nil
}

func newTest(st *fakeStore, f Fetcher) *Refresher {
	r := New(st, f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.sleep = func(context.Context, time.Duration) bool { return true }
	return r
}

func newStore(ids ...string) *fakeStore {
	return &fakeStore{ids: ids, set: map[string]*domain.AlbumDetails{}, deferred: map[string]time.Duration{}}
}

func TestDefaults(t *testing.T) {
	r := New(nil, nil, nil)
	if r.Interval != time.Minute || r.Spacing != 2*time.Second || r.Batch != 15 || r.MaxAge != 180*24*time.Hour ||
		r.RetryIn != time.Hour {
		t.Errorf("defaults = %+v", r)
	}
}

func TestRunOnceStoresDetails(t *testing.T) {
	st := newStore("al1", "missing", "al2")
	if pause := newTest(st, fakeFetcher{}).RunOnce(context.Background()); pause != 0 {
		t.Fatalf("pause = %v", pause)
	}
	for _, id := range []string{"al1", "al2"} {
		if d := st.set[id]; d == nil || d.UPC != "upc-"+id {
			t.Errorf("%s: got %+v", id, d)
		}
	}
	if d, ok := st.set["missing"]; !ok || d != nil {
		t.Errorf("missing: got %+v (recorded %v), want recorded nil", d, ok)
	}
}

func TestRunOnceRecordsPermanentError(t *testing.T) {
	st := newStore("bad", "al1")
	newTest(st, fakeFetcher{"bad": &spotify.StatusError{Code: 400}}).RunOnce(context.Background())
	if d, ok := st.set["bad"]; !ok || d != nil {
		t.Errorf("bad: got %+v (recorded %v), want recorded nil", d, ok)
	}
	if st.set["al1"] == nil {
		t.Error("al1 not fetched after a 400")
	}
}

// A transient failure defers that album by about RetryIn (so it leaves the
// head of the queue) without recording details, and the batch goes on.
func TestRunOnceDefersTransientError(t *testing.T) {
	for _, err := range []error{&spotify.StatusError{Code: 503}, errors.New("network down")} {
		st := newStore("down", "al1")
		r := newTest(st, fakeFetcher{"down": err})
		if pause := r.RunOnce(context.Background()); pause != 0 {
			t.Errorf("%v: pause = %v", err, pause)
		}
		if _, ok := st.set["down"]; ok {
			t.Errorf("%v: recorded details for a failed lookup", err)
		}
		if age, ok := st.deferred["down"]; !ok || age != r.MaxAge-time.Hour {
			t.Errorf("%v: deferred %v (%v), want MaxAge - 1h", err, age, ok)
		}
		if st.set["al1"] == nil {
			t.Errorf("%v: al1 not fetched after a transient error", err)
		}
	}
}

// Three transient failures in a row end the batch; a success resets the count.
func TestRunOnceStopsAfterRepeatedTransientErrors(t *testing.T) {
	down := errors.New("network down")
	st := newStore("d1", "d2", "ok", "d3", "d4", "d5", "after")
	newTest(st, fakeFetcher{"d1": down, "d2": down, "d3": down, "d4": down, "d5": down}).RunOnce(context.Background())
	if st.set["ok"] == nil {
		t.Error("ok not fetched: success did not reset the failure count")
	}
	if _, ok := st.set["after"]; ok {
		t.Error("kept fetching after three failures in a row")
	}
	if len(st.deferred) != 5 {
		t.Errorf("deferred = %v", st.deferred)
	}
}

func TestRunOnceBacksOffOnRateLimit(t *testing.T) {
	st := newStore("al1", "limited", "al2")
	f := fakeFetcher{"limited": &spotify.RateLimitError{RetryAfter: 90 * time.Second}}
	if pause := newTest(st, f).RunOnce(context.Background()); pause != 90*time.Second {
		t.Fatalf("pause = %v, want 90s", pause)
	}
	if _, ok := st.set["limited"]; ok {
		t.Error("recorded a rate-limited lookup as done")
	}
	if _, ok := st.set["al2"]; ok {
		t.Error("kept fetching after a rate limit")
	}
}

func TestRunOnceNotConnectedRecordsNothing(t *testing.T) {
	for _, err := range []error{spotify.ErrNotConnected, spotify.ErrInvalidGrant} {
		st := newStore("al1", "al2")
		if pause := newTest(st, fakeFetcher{"al1": err}).RunOnce(context.Background()); pause != 0 {
			t.Errorf("%v: pause = %v", err, pause)
		}
		if len(st.set) != 0 {
			t.Errorf("%v: recorded lookups: %v", err, st.set)
		}
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
