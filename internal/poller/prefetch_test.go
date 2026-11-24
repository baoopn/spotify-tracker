package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

type fakeQueue struct {
	mu      sync.Mutex
	entries []domain.CatalogEntry
	err     error
	calls   int
	block   chan struct{} // when set, Queue waits for it
}

func (f *fakeQueue) Queue(ctx context.Context, _ int) ([]domain.CatalogEntry, error) {
	f.mu.Lock()
	f.calls++
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entries, f.err
}

func (f *fakeQueue) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeIngest struct {
	mu   sync.Mutex
	reqs []*trackerv1.PrefetchUpcomingRequest
	err  error
}

func (f *fakeIngest) RecordPlayback(context.Context, *trackerv1.RecordPlaybackRequest, ...grpc.CallOption) (*trackerv1.RecordPlaybackResponse, error) {
	return &trackerv1.RecordPlaybackResponse{}, nil
}

func (f *fakeIngest) ClearPlayback(context.Context, *trackerv1.ClearPlaybackRequest, ...grpc.CallOption) (*trackerv1.ClearPlaybackResponse, error) {
	return &trackerv1.ClearPlaybackResponse{}, nil
}

func (f *fakeIngest) PrefetchUpcoming(_ context.Context, r *trackerv1.PrefetchUpcomingRequest, _ ...grpc.CallOption) (*trackerv1.PrefetchUpcomingResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r)
	if f.err != nil {
		return nil, f.err
	}
	res := &trackerv1.PrefetchUpcomingResponse{}
	for _, t := range r.GetTracks() {
		if t.GetPrefetch() {
			res.PrefetchedTrackIds = append(res.PrefetchedTrackIds, t.GetTrack().GetId())
		}
	}
	return res, nil
}

func (f *fakeIngest) last() *trackerv1.PrefetchUpcomingRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		return nil
	}
	return f.reqs[len(f.reqs)-1]
}

func entry(id string) domain.CatalogEntry {
	return domain.CatalogEntry{Track: domain.Track{ID: id, Name: "song " + id}}
}

type harness struct {
	p     *Poller
	f     *prefetcher
	q     *fakeQueue
	in    *fakeIngest
	clock time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{q: &fakeQueue{}, in: &fakeIngest{}, clock: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	h.p = New(nil, h.in, 3*time.Second, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.p.EnablePrefetch(h.q, PrefetchConfig{Depth: 1, Recheck: time.Minute, TTL: time.Hour})
	h.f = h.p.prefetch
	h.f.now = func() time.Time { return h.clock }
	return h
}

// kickWait kicks and waits for the background read to finish.
func (h *harness) kickWait(id string) {
	h.f.kick(context.Background(), id)
	for h.f.running.Load() {
		time.Sleep(time.Millisecond)
	}
}

func TestPrefetchOnTrackChangeAndRecheck(t *testing.T) {
	h := newHarness(t)
	h.q.entries = []domain.CatalogEntry{entry("next1"), entry("next2")}
	h.kickWait("cur")
	if h.q.count() != 1 {
		t.Fatalf("calls = %d", h.q.count())
	}
	r := h.in.last()
	if r.GetCurrentTrackId() != "cur" || len(r.GetTracks()) != 1 || r.GetTracks()[0].GetTrack().GetId() != "next1" ||
		!r.GetTracks()[0].GetPrefetch() || r.GetTracks()[0].GetPosition() != 0 {
		t.Fatalf("req = %+v", r)
	}
	h.clock = h.clock.Add(30 * time.Second)
	h.kickWait("cur") // same song, before Recheck
	if h.q.count() != 1 {
		t.Fatalf("re-read too early: %d", h.q.count())
	}
	h.clock = h.clock.Add(31 * time.Second)
	h.kickWait("cur")
	if h.q.count() != 2 {
		t.Fatalf("recheck missing: %d", h.q.count())
	}
	if h.in.last().GetTracks()[0].GetPrefetch() {
		t.Error("recently prefetched track sent with prefetch=true")
	}
	h.kickWait("other") // track change reads at once
	if h.q.count() != 3 {
		t.Fatalf("track change did not read: %d", h.q.count())
	}
}

func TestPrefetchTTLExpires(t *testing.T) {
	h := newHarness(t)
	h.q.entries = []domain.CatalogEntry{entry("n")}
	h.kickWait("a")
	h.clock = h.clock.Add(2 * time.Hour)
	h.kickWait("b")
	if !h.in.last().GetTracks()[0].GetPrefetch() {
		t.Error("expired entry should be prefetched again")
	}
}

func TestPrefetchSkipsCurrentAndKeepsDepth(t *testing.T) {
	h := newHarness(t)
	h.q.entries = []domain.CatalogEntry{entry("cur"), entry("n1"), entry("n2")}
	h.kickWait("cur")
	r := h.in.last()
	if len(r.GetTracks()) != 1 || r.GetTracks()[0].GetTrack().GetId() != "n1" {
		t.Fatalf("req = %+v", r.GetTracks())
	}
}

func TestPrefetchEmptyQueueStillSent(t *testing.T) {
	h := newHarness(t)
	h.q.entries = []domain.CatalogEntry{}
	h.kickWait("cur")
	r := h.in.last()
	if r == nil || len(r.GetTracks()) != 0 {
		t.Fatalf("empty queue must still be reported: %+v", r)
	}
}

func TestPrefetchFailedRPCMarksNothing(t *testing.T) {
	h := newHarness(t)
	h.q.entries = []domain.CatalogEntry{entry("n")}
	h.in.err = errors.New("unavailable")
	h.kickWait("a")
	h.in.err = nil
	h.kickWait("b")
	if !h.in.last().GetTracks()[0].GetPrefetch() {
		t.Error("track was marked done after a failed RPC")
	}
}

func TestPrefetchInFlightBlocksSecond(t *testing.T) {
	h := newHarness(t)
	h.q.entries = []domain.CatalogEntry{entry("n")}
	h.q.block = make(chan struct{})
	h.f.kick(context.Background(), "a")
	for h.q.count() == 0 {
		time.Sleep(time.Millisecond)
	}
	h.f.kick(context.Background(), "b") // in flight: ignored
	close(h.q.block)
	for h.f.running.Load() {
		time.Sleep(time.Millisecond)
	}
	if h.q.count() != 1 {
		t.Fatalf("calls = %d", h.q.count())
	}
}

func TestPrefetchRateLimitPausesPoller(t *testing.T) {
	h := newHarness(t)
	h.q.err = &spotify.RateLimitError{RetryAfter: 2 * time.Minute}
	h.kickWait("a")
	if got := time.Unix(0, h.p.pauseUntil.Load()); !got.Equal(h.clock.Add(2 * time.Minute)) {
		t.Errorf("pauseUntil = %v", got)
	}
	h.clock = h.clock.Add(90 * time.Second)
	h.kickWait("b") // still before retryAt
	if h.q.count() != 1 {
		t.Fatalf("read during the rate limit: %d", h.q.count())
	}
	h.clock = h.clock.Add(31 * time.Second)
	h.q.err = nil
	h.kickWait("b")
	if h.q.count() != 2 {
		t.Fatalf("no read after the rate limit: %d", h.q.count())
	}
}

func TestPrefetchForbiddenBacksOffAnHour(t *testing.T) {
	h := newHarness(t)
	h.q.err = spotify.ErrQueueForbidden
	h.kickWait("a")
	h.clock = h.clock.Add(59 * time.Minute)
	h.kickWait("b")
	if h.q.count() != 1 {
		t.Fatalf("read during the 403 backoff: %d", h.q.count())
	}
	h.clock = h.clock.Add(2 * time.Minute)
	h.kickWait("c")
	if h.q.count() != 2 {
		t.Fatalf("no read after the backoff: %d", h.q.count())
	}
}

func TestPrefetchIdleResets(t *testing.T) {
	h := newHarness(t)
	h.q.entries = []domain.CatalogEntry{entry("n")}
	h.kickWait("a")
	h.f.idle()
	h.clock = h.clock.Add(5 * time.Second)
	h.kickWait("a") // same song after idle: read again
	if h.q.count() != 2 {
		t.Fatalf("calls = %d", h.q.count())
	}
}

func TestPrefetchPruneBounded(t *testing.T) {
	h := newHarness(t)
	for i := range maxDoneEntries + 50 {
		h.f.done[string(rune('a'+i%26))+time.Duration(i).String()] = h.clock.Add(-time.Duration(i) * time.Second)
	}
	h.f.prune(h.clock)
	if len(h.f.done) > maxDoneEntries {
		t.Fatalf("len = %d", len(h.f.done))
	}
}
