package lyrics

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/lrclib"
)

type deferred struct {
	id, status, msg string
	next            time.Time
}

type fakeStore struct {
	cands       []domain.LyricsCandidate
	listErr     error
	setErr      error
	lost        map[string]bool // SetLyrics finds the claim gone
	retranslate map[string]bool // SetLyrics re-queued the translation
	set         map[string]domain.Lyrics
	deferred    []deferred
	released    []string
	priority    []string // last priority asked for
	owners      []string // owner of every call
	lease       time.Duration
	limit       int
}

func (f *fakeStore) ClaimLyrics(_ context.Context, owner string, priority []string, lease time.Duration, limit int) ([]domain.LyricsCandidate, error) {
	f.priority, f.lease, f.limit = priority, lease, limit
	f.owners = append(f.owners, owner)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.cands[:min(limit, len(f.cands))], nil
}

func (f *fakeStore) SetLyrics(_ context.Context, owner, id string, l domain.Lyrics) (bool, bool, error) {
	f.owners = append(f.owners, owner)
	if f.setErr != nil {
		return false, false, f.setErr
	}
	if f.lost[id] {
		return false, false, nil
	}
	if f.set == nil {
		f.set = map[string]domain.Lyrics{}
	}
	f.set[id] = l
	return true, f.retranslate[id], nil
}

func (f *fakeStore) DeferLyrics(_ context.Context, owner, id, status string, next time.Time, msg string) error {
	f.owners = append(f.owners, owner)
	f.deferred = append(f.deferred, deferred{id, status, msg, next})
	return nil
}

func (f *fakeStore) ReleaseLyrics(_ context.Context, owner, id string) error {
	f.owners = append(f.owners, owner)
	f.released = append(f.released, id)
	return nil
}

type result struct {
	l   *domain.Lyrics
	err error
}

type fakeSource struct {
	res    map[string]result
	calls  []string
	cancel context.CancelFunc // called on the first lookup when set
	during func(id string)    // called on every lookup when set
}

func (f *fakeSource) Lookup(_ context.Context, c domain.LyricsCandidate) (*domain.Lyrics, error) {
	f.calls = append(f.calls, c.TrackID)
	if f.cancel != nil {
		f.cancel()
	}
	if f.during != nil {
		f.during(c.TrackID)
	}
	r := f.res[c.TrackID]
	return r.l, r.err
}

var t0 = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func newTest(st *fakeStore, src *fakeSource) *Refresher {
	r := New(st, src, "w1", slog.New(slog.DiscardHandler))
	r.now = func() time.Time { return t0 }
	r.sleep = func(context.Context, time.Duration, <-chan struct{}) bool { return true }
	return r
}

func cands(ids ...string) []domain.LyricsCandidate {
	out := make([]domain.LyricsCandidate, len(ids))
	for i, id := range ids {
		out[i] = domain.LyricsCandidate{TrackID: id, Name: id}
	}
	return out
}

func TestDefaults(t *testing.T) {
	r := New(nil, nil, "w", nil)
	if r.Interval != time.Minute || r.Batch != 10 || r.NotFoundRetry != 720*time.Hour || r.Lease != 10*time.Minute ||
		r.MinBackoff != time.Hour || r.MaxBackoff != 168*time.Hour || r.MaxPause != time.Hour {
		t.Errorf("defaults = %+v", r)
	}
}

func TestRunOnceOutcomes(t *testing.T) {
	st := &fakeStore{cands: cands("found", "inst", "none", "flaky", "after")}
	st.cands[3].Status, st.cands[3].Attempts = domain.LyricsError, 2
	src := &fakeSource{res: map[string]result{
		"found": {l: &domain.Lyrics{Status: domain.LyricsFound, LRCLibID: 1, Plain: "x"}},
		"inst":  {l: &domain.Lyrics{Status: domain.LyricsInstrumental, LRCLibID: 2}},
		"flaky": {err: errors.New("boom " + strings.Repeat("é", 400))},
		"after": {l: &domain.Lyrics{Status: domain.LyricsFound, LRCLibID: 3, Plain: "y"}},
	}}
	if pause := newTest(st, src).RunOnce(context.Background()); pause != 0 {
		t.Fatalf("pause = %s", pause)
	}
	if len(st.set) != 3 || st.set["found"].LRCLibID != 1 || st.set["inst"].Status != domain.LyricsInstrumental ||
		st.set["after"].LRCLibID != 3 {
		t.Errorf("set = %+v", st.set)
	}
	if len(st.deferred) != 2 {
		t.Fatalf("deferred = %+v", st.deferred)
	}
	if d := st.deferred[0]; d.id != "none" || d.status != domain.LyricsNotFound || !d.next.Equal(t0.Add(720*time.Hour)) || d.msg != "" {
		t.Errorf("not found = %+v", d)
	}
	// attempts 2 → third failure → 1h·2² = 4h; the batch continues.
	d := st.deferred[1]
	if d.id != "flaky" || d.status != domain.LyricsError || !d.next.Equal(t0.Add(4*time.Hour)) {
		t.Errorf("error = %+v", d)
	}
	if len(d.msg) > maxErrLen || !strings.HasPrefix(d.msg, "boom") {
		t.Errorf("error message length %d", len(d.msg))
	}
	if strings.ToValidUTF8(d.msg, "?") != d.msg {
		t.Error("truncated message is not valid UTF-8")
	}
}

func TestThreeTransientErrorsStopTheBatch(t *testing.T) {
	st := &fakeStore{cands: cands("a", "b", "c", "d")}
	e := result{err: errors.New("timeout")}
	src := &fakeSource{res: map[string]result{"a": e, "b": e, "c": e, "d": e}}
	if pause := newTest(st, src).RunOnce(context.Background()); pause != 2*time.Minute {
		t.Errorf("pause = %s, want 2m after the first bad batch", pause)
	}
	if len(src.calls) != 3 || len(st.deferred) != 3 {
		t.Errorf("calls = %v, deferred = %d", src.calls, len(st.deferred))
	}
	if !slices.Equal(st.released, []string{"d"}) {
		t.Errorf("released = %v, want the unprocessed claim", st.released)
	}
}

func TestPermanentErrorIsNotTransient(t *testing.T) {
	st := &fakeStore{cands: cands("a", "b", "p", "c", "d")}
	e := result{err: errors.New("timeout")}
	src := &fakeSource{res: map[string]result{"a": e, "b": e, "p": {err: &lrclib.StatusError{Code: 400}}, "c": e, "d": e}}
	newTest(st, src).RunOnce(context.Background())
	if len(src.calls) != 4 {
		t.Errorf("calls = %v; a 4xx must not count toward the limit", src.calls)
	}
	if d := st.deferred[2]; d.id != "p" || d.status != domain.LyricsError || !d.next.Equal(t0.Add(168*time.Hour)) {
		t.Errorf("permanent = %+v", d)
	}
}

func TestSuccessResetsTransientCount(t *testing.T) {
	st := &fakeStore{cands: cands("a", "b", "ok", "c", "d", "e")}
	e := result{err: errors.New("timeout")}
	src := &fakeSource{res: map[string]result{"a": e, "b": e, "ok": {}, "c": e, "d": e, "e": e}}
	newTest(st, src).RunOnce(context.Background())
	if len(src.calls) != 6 {
		t.Errorf("calls = %v", src.calls)
	}
}

func TestRateLimitPausesAndWritesNothing(t *testing.T) {
	st := &fakeStore{cands: cands("a", "b")}
	src := &fakeSource{res: map[string]result{"a": {err: &lrclib.RateLimitError{RetryAfter: 90 * time.Second}}}}
	if pause := newTest(st, src).RunOnce(context.Background()); pause != 90*time.Second {
		t.Errorf("pause = %s", pause)
	}
	if len(src.calls) != 1 || len(st.deferred) != 0 || len(st.set) != 0 {
		t.Errorf("calls %v, deferred %v, set %v", src.calls, st.deferred, st.set)
	}
	if !slices.Equal(st.released, []string{"a", "b"}) {
		t.Errorf("released = %v, want both claims back", st.released)
	}
}

func TestCancelWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st := &fakeStore{cands: cands("a", "b")}
	src := &fakeSource{res: map[string]result{"a": {err: context.Canceled}}, cancel: cancel}
	if pause := newTest(st, src).RunOnce(ctx); pause != 0 {
		t.Errorf("pause = %s", pause)
	}
	if len(src.calls) != 1 || len(st.deferred) != 0 {
		t.Errorf("calls %v, deferred %v", src.calls, st.deferred)
	}
	if !slices.Equal(st.released, []string{"a", "b"}) {
		t.Errorf("released = %v; shutdown must give the claims back", st.released)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st := &fakeStore{listErr: errors.New("db down")}
	r := newTest(st, &fakeSource{})
	var waits []time.Duration
	r.sleep = func(_ context.Context, d time.Duration, _ <-chan struct{}) bool {
		waits = append(waits, d)
		cancel()
		return false
	}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] != time.Minute {
		t.Errorf("waits = %v", waits)
	}
}

func TestRunUsesRateLimitPause(t *testing.T) {
	st := &fakeStore{cands: cands("a")}
	src := &fakeSource{res: map[string]result{"a": {err: &lrclib.RateLimitError{RetryAfter: 10 * time.Minute}}}}
	r := newTest(st, src)
	var got time.Duration
	r.sleep = func(_ context.Context, d time.Duration, _ <-chan struct{}) bool { got = d; return false }
	_ = r.Run(context.Background())
	if got != 10*time.Minute {
		t.Errorf("wait = %s, want the Retry-After pause", got)
	}
}

func TestBackoff(t *testing.T) {
	lo, hi := time.Hour, 168*time.Hour
	for _, tc := range []struct {
		n    int32
		want time.Duration
	}{{0, time.Hour}, {1, time.Hour}, {2, 2 * time.Hour}, {3, 4 * time.Hour}, {7, 64 * time.Hour},
		{8, 128 * time.Hour}, {9, hi}, {40, hi}} {
		if got := backoff(tc.n, lo, hi); got != tc.want {
			t.Errorf("backoff(%d) = %s, want %s", tc.n, got, tc.want)
		}
	}
}

func TestStoreFailureParksTheTrack(t *testing.T) {
	st := &fakeStore{cands: cands("a", "b"), setErr: errors.New("invalid byte sequence")}
	st.cands[1].Status, st.cands[1].Attempts = domain.LyricsError, 1
	ok := result{l: &domain.Lyrics{Status: domain.LyricsFound, Plain: "x"}}
	src := &fakeSource{res: map[string]result{"a": ok, "b": ok}}
	newTest(st, src).RunOnce(context.Background())
	if len(st.deferred) != 2 {
		t.Fatalf("deferred = %+v", st.deferred)
	}
	if d := st.deferred[0]; d.id != "a" || d.status != domain.LyricsError || !d.next.Equal(t0.Add(time.Hour)) ||
		!strings.Contains(d.msg, "store lyrics: invalid byte sequence") {
		t.Errorf("a = %+v", d)
	}
	if d := st.deferred[1]; !d.next.Equal(t0.Add(2 * time.Hour)) {
		t.Errorf("b = %+v, want the second-error backoff", d)
	}
}

// A not-found record restarts the error backoff (attempts counted per status).
func TestRetryAtUsesErrorAttemptsOnly(t *testing.T) {
	r := newTest(&fakeStore{}, &fakeSource{})
	for _, tc := range []struct {
		status   string
		attempts int32
		want     time.Duration
	}{{"", 0, time.Hour}, {domain.LyricsNotFound, 5, time.Hour}, {domain.LyricsError, 1, 2 * time.Hour},
		{domain.LyricsError, 3, 8 * time.Hour}, {domain.LyricsFound, 0, time.Hour}, {domain.LyricsFound, 2, 4 * time.Hour},
		{domain.LyricsInstrumental, 1, 2 * time.Hour}} {
		got := r.retryAt(domain.LyricsCandidate{Status: tc.status, Attempts: tc.attempts}).Sub(t0)
		if got != tc.want {
			t.Errorf("%s/%d = %s, want %s", tc.status, tc.attempts, got, tc.want)
		}
	}
}

func TestBadBatchPauseGrowsAndResets(t *testing.T) {
	e := result{err: errors.New("timeout")}
	st := &fakeStore{cands: cands("a", "b", "c")}
	src := &fakeSource{res: map[string]result{"a": e, "b": e, "c": e}}
	r := newTest(st, src)
	var got []time.Duration
	for range 7 {
		got = append(got, r.RunOnce(context.Background()))
	}
	want := []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute,
		32 * time.Minute, time.Hour, time.Hour}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pauses = %v, want %v", got, want)
		}
	}
	// A batch with a successful lookup resets the pause.
	src.res["a"] = result{}
	if p := r.RunOnce(context.Background()); p != 0 {
		t.Errorf("good batch pause = %s", p)
	}
	src.res["a"] = e
	if p := r.RunOnce(context.Background()); p != 2*time.Minute {
		t.Errorf("after reset = %s, want 2m", p)
	}
	// Run waits for the pause.
	var waited time.Duration
	r.sleep = func(_ context.Context, d time.Duration, _ <-chan struct{}) bool { waited = d; return false }
	_ = r.Run(context.Background())
	if waited != 4*time.Minute {
		t.Errorf("Run waited %s, want 4m", waited)
	}
}

// Wake makes the song playing now the priority of the next batch and starts
// it early; the same song twice signals once.
func TestRefresherWake(t *testing.T) {
	st := &fakeStore{cands: []domain.LyricsCandidate{{TrackID: "now"}}}
	src := &fakeSource{res: map[string]result{"now": {l: &domain.Lyrics{Status: domain.LyricsFound, Plain: "x"}}}}
	r := New(st, src, "w1", slog.New(slog.DiscardHandler))
	var found []string
	r.OnFound = func(id string) { found = append(found, id) }
	r.Wake("now")
	r.Wake("now")
	if len(r.wake) != 1 {
		t.Fatalf("wake signals = %d, want 1", len(r.wake))
	}
	r.RunOnce(context.Background())
	if !slices.Equal(st.priority, []string{"now"}) {
		t.Errorf("priority = %q", st.priority)
	}
	if len(found) != 1 || found[0] != "now" {
		t.Errorf("OnFound = %v", found)
	}
	<-r.wake
	r.Wake("now")
	if len(r.wake) != 0 {
		t.Error("same song woke the refresher again")
	}
	r.Wake("")
	if len(r.wake) != 0 || !slices.Equal(st.priority, []string{"now"}) {
		t.Error("empty id must be ignored")
	}
}

// WakeUpcoming puts the queued songs right after the song playing now and
// starts the next batch early.
func TestRefresherWakeUpcoming(t *testing.T) {
	st := &fakeStore{}
	r := New(st, &fakeSource{}, "w1", slog.New(slog.DiscardHandler))
	r.Wake("now")
	<-r.wake
	r.WakeUpcoming([]string{"next1", "now", "next2"})
	if len(r.wake) != 1 {
		t.Fatalf("wake signals = %d", len(r.wake))
	}
	r.RunOnce(context.Background())
	if want := []string{"now", "next1", "next2"}; !slices.Equal(st.priority, want) {
		t.Errorf("priority = %q, want %q", st.priority, want)
	}
	r.WakeUpcoming(nil)
	if len(r.wake) != 1 {
		t.Error("empty list must not wake")
	}
}

// The upcoming list drops a song once it plays, clears on idle (nil) and
// expires after UpcomingTTL.
func TestRefresherUpcomingLifetime(t *testing.T) {
	st := &fakeStore{}
	r := New(st, &fakeSource{}, "w1", slog.New(slog.DiscardHandler))
	clock := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return clock }
	r.WakeUpcoming([]string{"a", "b"})
	r.Wake("a")
	if got := r.priorityList(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("after a plays = %v", got)
	}
	r.mu.Lock()
	up := slices.Clone(r.upcoming)
	r.mu.Unlock()
	if !slices.Equal(up, []string{"b"}) {
		t.Errorf("upcoming = %v", up)
	}
	r.WakeUpcoming(nil)
	if got := r.priorityList(); !slices.Equal(got, []string{"a"}) {
		t.Errorf("after idle = %v", got)
	}
	r.WakeUpcoming([]string{"c"})
	clock = clock.Add(UpcomingTTL + time.Second)
	if got := r.priorityList(); !slices.Equal(got, []string{"a"}) {
		t.Errorf("after expiry = %v", got)
	}
}

// Every store call uses the refresher owner, and a batch is claimed with
// the lease, the batch size and the priority list.
func TestClaimUsesOwnerLeaseAndBatch(t *testing.T) {
	st := &fakeStore{cands: cands("a", "b")}
	src := &fakeSource{res: map[string]result{"a": {l: &domain.Lyrics{Status: domain.LyricsFound, Plain: "x"}}}}
	r := newTest(st, src)
	r.Lease, r.Batch = 7*time.Minute, 4
	r.Wake("now")
	r.RunOnce(context.Background())
	if st.lease != 7*time.Minute || st.limit != 4 || !slices.Equal(st.priority, []string{"now"}) {
		t.Errorf("claim lease %s limit %d priority %v", st.lease, st.limit, st.priority)
	}
	if len(st.owners) != 3 { // claim, set a, defer b
		t.Fatalf("calls = %v", st.owners)
	}
	for _, o := range st.owners {
		if o != "w1" {
			t.Errorf("owner = %q", o)
		}
	}
	if len(st.released) != 0 {
		t.Errorf("released = %v; finished claims need no release", st.released)
	}
}

// A new song (or an admin re-queue) during a batch stops it: the rest of the
// claims go back, so the next batch claims in the new order right away.
func TestNewSongStopsTheBatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		poke func(r *Refresher)
	}{
		{"wake", func(r *Refresher) { r.Wake("other") }},
		{"nudge", func(r *Refresher) { r.Nudge() }},
	} {
		st := &fakeStore{cands: cands("a", "b", "c")}
		src := &fakeSource{res: map[string]result{}}
		r := newTest(st, src)
		src.during = func(id string) {
			if id == "a" {
				tc.poke(r)
			}
		}
		if p := r.RunOnce(context.Background()); p != 0 {
			t.Errorf("%s: pause = %s", tc.name, p)
		}
		if !slices.Equal(src.calls, []string{"a"}) || !slices.Equal(st.released, []string{"b", "c"}) {
			t.Errorf("%s: calls %v released %v", tc.name, src.calls, st.released)
		}
		if len(r.wake) != 1 {
			t.Errorf("%s: the next batch must start now", tc.name)
		}
	}
	// The same song again, or a new upcoming list, does not stop a batch.
	st := &fakeStore{cands: cands("a", "b")}
	src := &fakeSource{res: map[string]result{}}
	r := newTest(st, src)
	r.Wake("now")
	src.during = func(string) { r.Wake("now"); r.WakeUpcoming([]string{"x"}) }
	r.RunOnce(context.Background())
	if len(src.calls) != 2 || len(st.released) != 0 {
		t.Errorf("calls %v released %v", src.calls, st.released)
	}
}

// A result whose claim was lost (lease expired, another worker took the
// track) is not counted as found: no OnFound.
func TestLostClaimSkipsOnFound(t *testing.T) {
	st := &fakeStore{cands: cands("a", "b"), lost: map[string]bool{"a": true}}
	ok := result{l: &domain.Lyrics{Status: domain.LyricsFound, Plain: "x"}}
	src := &fakeSource{res: map[string]result{"a": ok, "b": ok}}
	r := newTest(st, src)
	var found []string
	r.OnFound = func(id string) { found = append(found, id) }
	r.RunOnce(context.Background())
	if !slices.Equal(found, []string{"b"}) || len(st.deferred) != 0 || len(st.released) != 0 {
		t.Errorf("found %v deferred %v released %v", found, st.deferred, st.released)
	}
}

// Changed lyrics that re-queued the translation still wake the translator
// (OnFound); instrumental lyrics never do.
func TestRetranslateWakesTranslator(t *testing.T) {
	st := &fakeStore{cands: cands("changed", "inst"), retranslate: map[string]bool{"changed": true}}
	src := &fakeSource{res: map[string]result{
		"changed": {l: &domain.Lyrics{Status: domain.LyricsFound, Plain: "new"}},
		"inst":    {l: &domain.Lyrics{Status: domain.LyricsInstrumental}},
	}}
	r := newTest(st, src)
	var found []string
	r.OnFound = func(id string) { found = append(found, id) }
	r.RunOnce(context.Background())
	if !slices.Equal(found, []string{"changed"}) {
		t.Errorf("OnFound = %v", found)
	}
}

// Nudge always signals (twice in a row it leaves one signal).
func TestNudgeSignals(t *testing.T) {
	r := newTest(&fakeStore{}, &fakeSource{})
	r.Nudge()
	r.Nudge()
	if len(r.wake) != 1 {
		t.Errorf("wake signals = %d, want 1", len(r.wake))
	}
}

// A rate-limit pause shorter than Interval still waits Interval, and no wake
// (a new song or a re-queue) can start a batch inside the Retry-After window.
func TestRateLimitPauseIgnoresWake(t *testing.T) {
	st := &fakeStore{cands: cands("a")}
	src := &fakeSource{res: map[string]result{"a": {err: &lrclib.RateLimitError{RetryAfter: 5 * time.Second}}}}
	r := newTest(st, src)
	var waits []time.Duration
	var wakes []bool
	r.sleep = func(_ context.Context, d time.Duration, wake <-chan struct{}) bool {
		waits, wakes = append(waits, d), append(wakes, wake != nil)
		r.Wake("other")
		r.Nudge()
		return len(waits) < 2
	}
	_ = r.Run(context.Background())
	if len(waits) != 2 || waits[0] != time.Minute || wakes[0] {
		t.Errorf("waits %v wakes %v; want 1m without a wake channel", waits, wakes)
	}
	// No pause: the normal wait can be cut short by a wake.
	st2 := &fakeStore{}
	r2 := newTest(st2, &fakeSource{})
	var gotWake bool
	r2.sleep = func(_ context.Context, _ time.Duration, wake <-chan struct{}) bool {
		gotWake = wake != nil
		return false
	}
	_ = r2.Run(context.Background())
	if !gotWake {
		t.Error("the normal wait must listen for wakes")
	}
}
