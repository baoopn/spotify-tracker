package translate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

var workerNow = time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)

type deferCall struct {
	id, status, msg string
	next            *time.Time
}

type queued struct {
	c            domain.TranslationCandidate
	claimedBy    string
	claimedUntil time.Time
	closed       bool // done, skipped, refused or given up
}

// fakeStore is an in-memory queue with the same lease rules as the SQL: a
// row is claimable when it is open and its lease is free or expired.
type fakeStore struct {
	mu       sync.Mutex
	now      time.Time
	lyrics   map[string]domain.TranslationCandidate // tracks with found lyrics
	recent   []string                               // played within the window
	queue    []*queued
	set      map[string]domain.Translation
	deferred []deferCall
	released []string
	calls    int
	capLeft  int // -1 = no cap
	claimErr error
	tokens   [2]int64 // input, output recorded
	// enqueueCalls counts EnqueueTranslation calls.
	enqueueCalls int
}

func cand(id, synced string) domain.TranslationCandidate {
	return domain.TranslationCandidate{TrackID: id, Name: "Song " + id, Artist: "A", Synced: synced, SourceHash: "h-" + id}
}

// newFakeStore queues cs (as if they had played).
func newFakeStore(cs ...domain.TranslationCandidate) *fakeStore {
	f := &fakeStore{now: workerNow, lyrics: map[string]domain.TranslationCandidate{},
		set: map[string]domain.Translation{}, capLeft: -1}
	for _, c := range cs {
		f.lyrics[c.TrackID] = c
		f.queue = append(f.queue, &queued{c: c})
	}
	return f
}

func (f *fakeStore) find(id string) *queued {
	for _, q := range f.queue {
		if q.c.TrackID == id {
			return q
		}
	}
	return nil
}

func (f *fakeStore) EnqueueTranslation(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueueCalls++
	if f.find(id) != nil {
		return true, nil
	}
	c, ok := f.lyrics[id]
	if !ok {
		return false, nil
	}
	f.queue = append(f.queue, &queued{c: c})
	return true, nil
}

func (f *fakeStore) EnqueueRecentTranslations(context.Context, time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, id := range f.recent {
		if c, ok := f.lyrics[id]; ok && f.find(id) == nil {
			f.queue = append(f.queue, &queued{c: c})
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) ClaimTranslations(_ context.Context, owner string, prio []string, lease time.Duration, limit int) ([]domain.TranslationCandidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	var free []*queued
	for _, q := range f.queue {
		if !q.closed && !q.claimedUntil.After(f.now) {
			free = append(free, q)
		}
	}
	rank := func(id string) int {
		if i := slices.Index(prio, id); i >= 0 {
			return i
		}
		return len(prio)
	}
	slices.SortStableFunc(free, func(a, b *queued) int { return rank(a.c.TrackID) - rank(b.c.TrackID) })
	var out []domain.TranslationCandidate
	for _, q := range free[:min(limit, len(free))] {
		q.claimedBy, q.claimedUntil = owner, f.now.Add(lease)
		out = append(out, q.c)
	}
	return out, nil
}

func (f *fakeStore) SetTranslation(_ context.Context, owner, id, _ string, r domain.Translation) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := f.find(id)
	if q == nil || q.claimedBy != owner {
		return false, nil
	}
	q.closed, q.claimedBy, q.claimedUntil = true, "", time.Time{}
	f.set[id] = r
	return true, nil
}

func (f *fakeStore) DeferTranslation(_ context.Context, owner, id, _, status string, next *time.Time, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deferred = append(f.deferred, deferCall{id, status, msg, next})
	if q := f.find(id); q != nil && q.claimedBy == owner {
		q.c.Status, q.claimedBy, q.claimedUntil = status, "", time.Time{}
		q.c.Attempts++
		switch {
		case status != domain.TranslationError || next == nil:
			q.closed = true
		default:
			q.claimedUntil = *next // not due before next
		}
	}
	return nil
}

func (f *fakeStore) ReleaseTranslation(_ context.Context, owner, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, id)
	if q := f.find(id); q != nil && q.claimedBy == owner {
		q.claimedBy, q.claimedUntil = "", time.Time{}
	}
	return nil
}

func (f *fakeStore) ReserveTranslationCall(context.Context, time.Time, int, int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.capLeft == 0 {
		return false, nil
	}
	if f.capLeft > 0 {
		f.capLeft--
	}
	f.calls++
	return true, nil
}

func (f *fakeStore) AddTranslationTokens(_ context.Context, _ time.Time, in, out int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[0] += in
	f.tokens[1] += out
	return nil
}

type fakeTranslator struct {
	errs  map[string]error
	order []string
}

func (f *fakeTranslator) Translate(_ context.Context, r Request) (Result, error) {
	f.order = append(f.order, r.TrackID)
	if err := f.errs[r.TrackID]; err != nil {
		var ae *APIError
		if errors.As(err, &ae) {
			return Result{}, err // the API refused: nothing billed
		}
		return Result{InputTokens: 7, OutputTokens: 3}, err
	}
	en := make([]string, len(r.Lines))
	for i, l := range r.Lines {
		if l != "" {
			en[i] = "EN " + l
		}
	}
	return Result{SourceLanguage: "vi", EN: en, Model: "claude-sonnet-5-5", InputTokens: 10, OutputTokens: 5}, nil
}

func testWorker(st Store, tr Translator, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	w := New(st, tr, "w1", log)
	w.now = func() time.Time { return workerNow }
	w.sleep = func(ctx context.Context, _ time.Duration, _ <-chan struct{}) bool { return ctx.Err() == nil }
	return w
}

func TestWorkerTranslates(t *testing.T) {
	st := newFakeStore(cand("a", "[00:01.00] một\n[00:02.00]\n[00:03.00] hai"))
	w := testWorker(st, &fakeTranslator{}, nil)
	if pause := w.RunOnce(context.Background()); pause != 0 {
		t.Fatalf("pause = %s", pause)
	}
	got, ok := st.set["a"]
	if !ok || got.Kind != domain.TranslationSynced || len(got.EN) != 3 || got.EN[0] != "EN một" || got.EN[1] != "" {
		t.Errorf("stored = %+v", got)
	}
	if st.calls != 1 {
		t.Errorf("calls = %d", st.calls)
	}
}

// The song playing now jumps the queue; it is queued on Wake. Songs that
// never played are never queued (no backfill).
func TestWorkerPriority(t *testing.T) {
	st := newFakeStore(cand("old", "[00:01.00] x"))
	st.lyrics["now"] = cand("now", "[00:01.00] y")
	st.lyrics["never"] = cand("never", "[00:01.00] z")
	tr := &fakeTranslator{}
	w := testWorker(st, tr, nil)
	w.Wake("now")
	w.RunOnce(context.Background())
	if len(tr.order) != 2 || tr.order[0] != "now" || tr.order[1] != "old" {
		t.Errorf("order = %v", tr.order)
	}
	if _, ok := st.set["never"]; ok {
		t.Error("a song that never played was translated")
	}
}

func TestWorkerRecentWindow(t *testing.T) {
	st := newFakeStore()
	st.lyrics["r"] = cand("r", "[00:01.00] x")
	st.recent = []string{"r"}
	testWorker(st, &fakeTranslator{}, nil).RunOnce(context.Background())
	if _, ok := st.set["r"]; !ok {
		t.Error("recent play not translated")
	}

	st = newFakeStore()
	st.lyrics["r"] = cand("r", "[00:01.00] x")
	st.recent = []string{"r"}
	w := testWorker(st, &fakeTranslator{}, nil)
	w.RecentWindow = 0
	w.RunOnce(context.Background())
	if len(st.set) != 0 {
		t.Error("RecentWindow=0 must queue only the song playing now")
	}
}

func TestWorkerWake(t *testing.T) {
	w := testWorker(newFakeStore(), &fakeTranslator{}, nil)
	w.Wake("a")
	w.Wake("a")
	if len(w.wake) != 1 {
		t.Fatalf("signals = %d", len(w.wake))
	}
	<-w.wake
	w.Wake("a")
	if len(w.wake) != 0 {
		t.Error("same song signalled again")
	}
	w.Nudge()
	w.Nudge()
	if len(w.wake) != 1 {
		t.Error("nudge must not block or queue twice")
	}
	w.Wake("")
	if w.priority != "a" {
		t.Error("empty id must be ignored")
	}
}

// Run starts the next batch on a wake instead of waiting the interval.
func TestWorkerRunWakes(t *testing.T) {
	st := newFakeStore()
	st.lyrics["now"] = cand("now", "[00:01.00] y")
	tr := &fakeTranslator{}
	w := testWorker(st, tr, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batches := 0
	w.sleep = func(ctx context.Context, d time.Duration, wake <-chan struct{}) bool {
		if d == w.Interval {
			batches++
			if batches == 1 {
				w.Wake("now")
			}
			select {
			case <-wake:
				return true
			default:
				cancel()
				return false
			}
		}
		return true
	}
	_ = w.Run(ctx)
	if len(tr.order) != 1 || tr.order[0] != "now" {
		t.Errorf("order = %v after %d batches", tr.order, batches)
	}
}

// A live lease is skipped; an expired one is claimed again.
func TestWorkerLease(t *testing.T) {
	st := newFakeStore(cand("a", "[00:01.00] x"))
	st.queue[0].claimedBy, st.queue[0].claimedUntil = "other", workerNow.Add(time.Minute)
	tr := &fakeTranslator{}
	w := testWorker(st, tr, nil)
	w.RunOnce(context.Background())
	if len(tr.order) != 0 {
		t.Fatalf("translated a song another worker holds: %v", tr.order)
	}
	st.queue[0].claimedUntil = workerNow.Add(-time.Second)
	w.RunOnce(context.Background())
	if len(tr.order) != 1 || st.set["a"].Model == "" {
		t.Errorf("expired lease not reclaimed: %v", tr.order)
	}
}

func TestWorkerOutcomes(t *testing.T) {
	ctx := context.Background()
	x := "[00:01.00] x"
	t.Run("rate limit releases and pauses", func(t *testing.T) {
		st := newFakeStore(cand("a", x))
		w := testWorker(st, &fakeTranslator{errs: map[string]error{"a": &APIError{Status: 429, RetryAfter: 2 * time.Minute}}}, nil)
		if p := w.RunOnce(ctx); p != 2*time.Minute {
			t.Errorf("pause = %s", p)
		}
		if len(st.released) != 1 || len(st.deferred) != 0 {
			t.Errorf("released %v deferred %v", st.released, st.deferred)
		}
	})
	t.Run("spend limit and auth pause long", func(t *testing.T) {
		for _, e := range []*APIError{{Status: 402}, {Status: 401}, {Status: 429, Code: "enforced_spend_limit_reached"}} {
			st := newFakeStore(cand("a", x))
			w := testWorker(st, &fakeTranslator{errs: map[string]error{"a": e}}, nil)
			if p := w.RunOnce(ctx); p != w.MaxPause || len(st.deferred) != 0 || len(st.released) != 1 {
				t.Errorf("%v: pause %s deferred %v", e, p, st.deferred)
			}
		}
	})
	t.Run("refusal is terminal", func(t *testing.T) {
		st := newFakeStore(cand("a", x))
		w := testWorker(st, &fakeTranslator{errs: map[string]error{"a": &RefusalError{Category: "bio"}}}, nil)
		w.RunOnce(ctx)
		if len(st.deferred) != 1 || st.deferred[0].status != domain.TranslationRefused || st.deferred[0].next != nil {
			t.Errorf("deferred = %+v", st.deferred)
		}
	})
	t.Run("three transient errors pause the batch", func(t *testing.T) {
		st := newFakeStore(cand("a", x), cand("b", x), cand("c", x), cand("d", x))
		boom := &APIError{Status: 529}
		tr := &fakeTranslator{errs: map[string]error{"a": boom, "b": boom, "c": boom, "d": boom}}
		w := testWorker(st, tr, nil)
		if p := w.RunOnce(ctx); p != 2*w.Interval || len(tr.order) != 3 {
			t.Errorf("pause %s after %v", p, tr.order)
		}
		if st.deferred[0].next == nil || !st.deferred[0].next.Equal(workerNow.Add(w.MinBackoff)) {
			t.Errorf("retry = %v", st.deferred[0].next)
		}
	})
	t.Run("permanent errors are not transient", func(t *testing.T) {
		st := newFakeStore(cand("a", x), cand("b", x), cand("c", x), cand("d", x))
		bad := &InvalidOutputError{Reason: "count"}
		tr := &fakeTranslator{errs: map[string]error{"a": bad, "b": bad, "c": &APIError{Status: 400}, "d": bad}}
		w := testWorker(st, tr, nil)
		if p := w.RunOnce(ctx); p != 0 || len(tr.order) != 4 {
			t.Errorf("pause %s after %v", p, tr.order)
		}
	})
	t.Run("gives up after MaxAttempts", func(t *testing.T) {
		c := cand("a", x)
		c.Status, c.Attempts = domain.TranslationError, 4
		st := newFakeStore(c)
		w := testWorker(st, &fakeTranslator{errs: map[string]error{"a": &APIError{Status: 500}}}, nil)
		w.RunOnce(ctx)
		if len(st.deferred) != 1 || st.deferred[0].next != nil {
			t.Errorf("deferred = %+v", st.deferred)
		}
	})
	t.Run("cap reached makes no call", func(t *testing.T) {
		st := newFakeStore(cand("a", x))
		st.capLeft = 0
		tr := &fakeTranslator{}
		w := testWorker(st, tr, nil)
		p := w.RunOnce(ctx)
		if len(tr.order) != 0 || p != 6*time.Hour || len(st.released) != 1 {
			t.Errorf("calls %v pause %s released %v", tr.order, p, st.released)
		}
	})
	t.Run("too long is skipped without a call", func(t *testing.T) {
		var b strings.Builder
		for i := range 500 {
			fmt.Fprintf(&b, "[%02d:%02d.00] line %d\n", i/60, i%60, i)
		}
		st := newFakeStore(cand("a", b.String()), domain.TranslationCandidate{TrackID: "e", SourceHash: "h"})
		tr := &fakeTranslator{}
		testWorker(st, tr, nil).RunOnce(ctx)
		if len(tr.order) != 0 || len(st.deferred) != 2 || st.deferred[0].status != domain.TranslationSkipped {
			t.Errorf("calls %v deferred %+v", tr.order, st.deferred)
		}
	})
	t.Run("cancel writes nothing", func(t *testing.T) {
		st := newFakeStore(cand("a", x))
		cctx, cancel := context.WithCancel(ctx)
		testWorker(st, &cancelTranslator{cancel: cancel}, nil).RunOnce(cctx)
		if len(st.set) != 0 || len(st.deferred) != 0 || len(st.released) != 1 {
			t.Errorf("set %v deferred %v released %v", st.set, st.deferred, st.released)
		}
	})
	t.Run("claim lost writes nothing", func(t *testing.T) {
		st := newFakeStore(cand("a", x))
		testWorker(st, &stealTranslator{st: st}, nil).RunOnce(ctx)
		if len(st.set) != 0 || len(st.deferred) != 0 {
			t.Errorf("set %v deferred %v", st.set, st.deferred)
		}
	})
	t.Run("claim error stops the batch", func(t *testing.T) {
		st := newFakeStore(cand("a", x))
		st.claimErr = errors.New("db down")
		if p := testWorker(st, &fakeTranslator{}, nil).RunOnce(ctx); p != 0 {
			t.Errorf("pause = %s", p)
		}
	})
}

type cancelTranslator struct{ cancel context.CancelFunc }

func (c *cancelTranslator) Translate(context.Context, Request) (Result, error) {
	c.cancel()
	return Result{}, context.Canceled
}

// stealTranslator lets another worker take the lease mid-call.
type stealTranslator struct{ st *fakeStore }

func (s *stealTranslator) Translate(_ context.Context, r Request) (Result, error) {
	s.st.find(r.TrackID).claimedBy = "other"
	return Result{SourceLanguage: "vi", EN: []string{"x"}}, nil
}

// Spacing is waited between calls, not before the first.
func TestWorkerSpacing(t *testing.T) {
	st := newFakeStore(cand("a", "[00:01.00] x"), cand("b", "[00:01.00] y"))
	w := testWorker(st, &fakeTranslator{}, nil)
	var waits []time.Duration
	w.sleep = func(_ context.Context, d time.Duration, _ <-chan struct{}) bool {
		waits = append(waits, d)
		return true
	}
	w.RunOnce(context.Background())
	if len(waits) != 1 || waits[0] != w.Spacing {
		t.Errorf("waits = %v", waits)
	}
}

func TestBuildRequest(t *testing.T) {
	r, ok := buildRequest(domain.TranslationCandidate{Plain: "  hello  ", Synced: "no tags"})
	if !ok || r.Kind != domain.TranslationPlain || r.Plain != "hello" {
		t.Errorf("plain = %+v %v", r, ok)
	}
	if _, ok := buildRequest(domain.TranslationCandidate{Synced: "[00:01.00]\n[00:02.00]  "}); ok {
		t.Error("only empty lines must be skipped")
	}
}

// A cut-off answer is not retried: the song is skipped as too long, and its
// billed tokens are recorded like those of unusable answers.
func TestWorkerTruncated(t *testing.T) {
	st := newFakeStore(cand("a", "[00:01.00] x"), cand("b", "[00:01.00] y"))
	tr := &fakeTranslator{errs: map[string]error{"a": ErrTruncated, "b": &InvalidOutputError{Reason: "count"}}}
	testWorker(st, tr, nil).RunOnce(context.Background())
	if len(st.deferred) != 2 || st.deferred[0].status != domain.TranslationSkipped || st.deferred[0].next != nil ||
		st.deferred[0].msg != "too long" {
		t.Errorf("deferred = %+v", st.deferred)
	}
	if st.deferred[1].status != domain.TranslationError || st.deferred[1].next == nil {
		t.Errorf("invalid output = %+v", st.deferred[1])
	}
	if st.tokens != [2]int64{14, 6} {
		t.Errorf("tokens = %v", st.tokens)
	}
}

// The song playing now is marked queued only once its row exists; when its
// lyrics arrive mid-play it is queued then, even with no recent window.
func TestWorkerLyricsFound(t *testing.T) {
	st := newFakeStore()
	tr := &fakeTranslator{}
	w := testWorker(st, tr, nil)
	w.RecentWindow = 0
	w.Wake("now")
	w.RunOnce(context.Background())
	if len(tr.order) != 0 || w.enqueued == "now" {
		t.Fatalf("translated %v, enqueued %q without lyrics", tr.order, w.enqueued)
	}
	st.lyrics["now"] = cand("now", "[00:01.00] y")
	<-w.wake
	w.LyricsFound("now")
	if len(w.wake) != 1 {
		t.Error("LyricsFound must start a batch")
	}
	w.RunOnce(context.Background())
	if len(tr.order) != 1 || tr.order[0] != "now" {
		t.Errorf("order = %v", tr.order)
	}
	// Once queued, the song is not enqueued on every batch.
	n := st.enqueueCalls
	w.RunOnce(context.Background())
	if st.enqueueCalls != n {
		t.Errorf("enqueued again: %d -> %d", n, st.enqueueCalls)
	}
}

// No spacing wait follows a song that needed no call.
func TestWorkerNoSpacingWithoutCall(t *testing.T) {
	st := newFakeStore(domain.TranslationCandidate{TrackID: "e", SourceHash: "h"}, cand("a", "[00:01.00] x"))
	w := testWorker(st, &fakeTranslator{}, nil)
	var waits []time.Duration
	w.sleep = func(_ context.Context, d time.Duration, _ <-chan struct{}) bool {
		waits = append(waits, d)
		return true
	}
	w.RunOnce(context.Background())
	if len(waits) != 0 || len(st.set) != 1 {
		t.Errorf("waits = %v, set = %v", waits, st.set)
	}
}

// Upcoming songs are queued once they have lyrics and claimed right after
// the song playing now, before anything else in the queue.
func TestWorkerEnqueueUpcoming(t *testing.T) {
	st := newFakeStore(cand("old", "[00:01.00] o"))
	st.lyrics["now"] = cand("now", "[00:01.00] n")
	st.lyrics["next"] = cand("next", "[00:01.00] x")
	tr := &fakeTranslator{}
	w := testWorker(st, tr, nil)
	w.RecentWindow = 0
	w.Wake("now")
	w.EnqueueUpcoming([]string{"next", "later"}) // later: no lyrics yet
	w.RunOnce(context.Background())
	if want := []string{"now", "next", "old"}; !slices.Equal(tr.order, want) {
		t.Fatalf("order = %v, want %v", tr.order, want)
	}
	if !w.upQueued["next"] || w.upQueued["later"] {
		t.Errorf("upQueued = %v", w.upQueued)
	}
	// later gets lyrics: LyricsFound clears its mark and it is queued.
	st.lyrics["later"] = cand("later", "[00:01.00] l")
	w.LyricsFound("later")
	w.RunOnce(context.Background())
	if tr.order[len(tr.order)-1] != "later" {
		t.Errorf("order = %v", tr.order)
	}
	w.EnqueueUpcoming(nil) // playback went idle: cleared
	if len(w.upcoming) != 0 {
		t.Errorf("upcoming = %v", w.upcoming)
	}
}

// An upcoming song that starts playing leaves the list; a list expires after
// UpcomingTTL, so stale ids never outrank re-queues; OnDone fires per stored
// translation.
func TestWorkerUpcomingExpiresAndDone(t *testing.T) {
	st := newFakeStore(cand("req", "[00:01.00] r"))
	st.lyrics["next"] = cand("next", "[00:01.00] x")
	tr := &fakeTranslator{}
	w := testWorker(st, tr, nil)
	w.RecentWindow = 0
	clock := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return clock }
	var done []string
	w.OnDone = func(id string) { done = append(done, id) }

	w.EnqueueUpcoming([]string{"next"})
	w.Wake("next") // it starts playing
	if len(w.upcoming) != 0 {
		t.Fatalf("upcoming after it plays = %v", w.upcoming)
	}

	w.EnqueueUpcoming([]string{"stale"})
	clock = clock.Add(UpcomingTTL + time.Minute)
	if got := w.enqueuePriority(context.Background()); !slices.Equal(got, []string{"next"}) {
		t.Errorf("priority after expiry = %v", got)
	}
	w.RunOnce(context.Background())
	if !slices.Equal(done, tr.order) || len(done) == 0 {
		t.Errorf("OnDone = %v, translated %v", done, tr.order)
	}
}
