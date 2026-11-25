package main

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/tracker"
)

type fakeUpcomingSvc struct {
	opts tracker.PrefetchOptions
	hook func([]string)
}

func (f *fakeUpcomingSvc) SetPrefetchOptions(o tracker.PrefetchOptions) { f.opts = o }
func (f *fakeUpcomingSvc) OnUpcoming(h func([]string))                  { f.hook = h }

type rec struct {
	mu    sync.Mutex
	calls [][]string
	wakes int
	warms []string
}

func (r *rec) WakeUpcoming(ids []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, slices.Clone(ids))
}

func (r *rec) EnqueueUpcoming(ids []string) { r.WakeUpcoming(ids) }

func (r *rec) Wake() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wakes++
}

func (r *rec) Warm(_ context.Context, id string, force bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if force {
		id += "!"
	}
	r.warms = append(r.warms, id)
}

func (r *rec) snapshot() ([][]string, int, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls), r.wakes, slices.Clone(r.warms)
}

// waitWarms waits for the asynchronous warm-ups.
func waitWarms(t *testing.T, r *rec, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, _, w := r.snapshot()
		if len(w) >= n || time.Now().After(deadline) {
			slices.Sort(w)
			return w
		}
		time.Sleep(time.Millisecond)
	}
}

func TestHookPrefetch(t *testing.T) {
	svc := &fakeUpcomingSvc{}
	lyr, trn, albums, warmer := &rec{}, &rec{}, &rec{}, &rec{}
	var onFound, onDone func(string)
	var prevFound []string
	cfg := config.API{Prefetch: config.Prefetch{Translate: true, Enrich: true, Details: true}}
	hookPrefetch(cfg, svc, prefetchHooks{
		lyrics: lyr, translator: trn, albums: albums, images: albums, warmer: warmer,
		setOnFound: func(wrap func(func(string)) func(string)) {
			onFound = wrap(func(id string) { prevFound = append(prevFound, id) })
		},
		setOnDone: func(wrap func(func(string)) func(string)) { onDone = wrap(nil) },
	}, slog.New(slog.DiscardHandler))
	if !svc.opts.Enrich {
		t.Error("enrich option not passed")
	}

	svc.hook([]string{"n1", "n2"})
	lc, _, _ := lyr.snapshot()
	tc, _, _ := trn.snapshot()
	_, wakes, _ := albums.snapshot()
	if !slices.Equal(lc[0], []string{"n1", "n2"}) {
		t.Errorf("lyrics = %v", lc)
	}
	if !slices.Equal(tc[0], []string{"n1"}) {
		t.Errorf("translator must get only the next song: %v", tc)
	}
	if wakes != 2 {
		t.Errorf("album/artist wakes = %d", wakes)
	}
	if w := waitWarms(t, warmer, 2); !slices.Equal(w, []string{"n1", "n2"}) {
		t.Errorf("warms = %v", w)
	}

	svc.hook([]string{"n1", "n2"}) // unchanged: no new wakes or warms
	if _, wakes, _ := albums.snapshot(); wakes != 2 {
		t.Errorf("unchanged list woke again: %d", wakes)
	}

	// OnFound chains to the previous hook and warms only upcoming tracks;
	// OnDone forces a rebuild.
	onFound("n2")
	onFound("other")
	onDone("n1")
	if !slices.Equal(prevFound, []string{"n2", "other"}) {
		t.Errorf("previous OnFound = %v", prevFound)
	}
	if w := waitWarms(t, warmer, 4); !slices.Equal(w, []string{"n1", "n1!", "n2", "n2"}) {
		t.Errorf("warms = %v", w)
	}

	svc.hook(nil) // idle: both lists cleared
	lc, _, _ = lyr.snapshot()
	tc, _, _ = trn.snapshot()
	if len(lc[len(lc)-1]) != 0 || len(tc[len(tc)-1]) != 0 {
		t.Errorf("idle did not clear: %v %v", lc, tc)
	}
}

// QUEUE_PREFETCH_TRANSLATE=false, DETAILS=false and missing workers.
func TestHookPrefetchGated(t *testing.T) {
	svc := &fakeUpcomingSvc{}
	trn, albums := &rec{}, &rec{}
	hookPrefetch(config.API{}, svc, prefetchHooks{translator: trn, albums: albums}, slog.New(slog.DiscardHandler))
	svc.hook([]string{"n1"})
	if tc, _, _ := trn.snapshot(); len(tc) != 0 {
		t.Errorf("translator called with translate off: %v", tc)
	}
	if _, wakes, _ := albums.snapshot(); wakes != 0 {
		t.Errorf("details woke with details off: %d", wakes)
	}
	if h := prefetchHooksFor(nil, nil, nil, nil, nil); h.lyrics != nil || h.translator != nil ||
		h.albums != nil || h.images != nil || h.warmer != nil || h.setOnFound != nil || h.setOnDone != nil {
		t.Errorf("typed nils leaked: %+v", h)
	}
}
