package translate

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/lrc"
)

// Store is the queue the worker needs (implemented by *store.Queries).
type Store interface {
	EnqueueTranslation(ctx context.Context, trackID string) (bool, error)
	EnqueueRecentTranslations(ctx context.Context, window time.Duration) (int64, error)
	ClaimTranslations(ctx context.Context, owner, priority string, lease time.Duration, limit int) ([]domain.TranslationCandidate, error)
	SetTranslation(ctx context.Context, owner, trackID, sourceHash string, r domain.Translation) (bool, error)
	DeferTranslation(ctx context.Context, owner, trackID, sourceHash, status string, next *time.Time, lastErr string) error
	ReleaseTranslation(ctx context.Context, owner, trackID string) error
	ReserveTranslationCall(ctx context.Context, day time.Time, dailyCap, monthlyCap int) (bool, error)
	AddTranslationTokens(ctx context.Context, day time.Time, in, out int64) error
}

// Request limits: the same cut as the public lyrics, and a size above which
// a song is skipped rather than sent.
const (
	maxRequestLines = 2000
	maxRequestRunes = 200 // per synced line
	maxSendLines    = 400 // non-empty lines
	maxSendRunes    = 15_000
	maxTransient    = 3
	maxErrLen       = 300
)

// Worker translates queued songs one at a time. Songs are queued only when
// they play (Wake with the song playing now, or a play within RecentWindow)
// or when an admin re-queues them; the library is never backfilled. Each
// song is claimed with a lease, so replicas never translate the same song.
type Worker struct {
	Store      Store
	Translator Translator
	Log        *slog.Logger
	Owner      string        // lease owner, unique per process
	Interval   time.Duration // between batches when nothing wakes the worker
	Batch      int           // songs per batch
	Spacing    time.Duration // between two Claude calls
	Lease      time.Duration // claim length; an expired claim can be taken again
	// RecentWindow queues songs played this recently that have lyrics and no
	// translation (0 = only the song playing now).
	RecentWindow time.Duration
	DailyCap     int // Claude calls per UTC day (0 = none)
	MonthlyCap   int // Claude calls per UTC month (0 = none)
	MinBackoff   time.Duration
	MaxBackoff   time.Duration
	MaxPause     time.Duration // longest pause (cap reached, spend limit, bad key)
	MaxAttempts  int32         // failures before giving up

	wake       chan struct{}
	mu         sync.Mutex
	priority   string // the song playing now: claimed first
	enqueued   string // the last priority song queued
	badBatches int
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration, wake <-chan struct{}) bool
}

// New returns a Worker with production defaults.
func New(st Store, tr Translator, owner string, log *slog.Logger) *Worker {
	return &Worker{Store: st, Translator: tr, Log: log, Owner: owner, Interval: time.Minute, Batch: 5,
		Spacing: 15 * time.Second, Lease: 10 * time.Minute, RecentWindow: 30 * time.Minute,
		DailyCap: 100, MonthlyCap: 1000, MinBackoff: 30 * time.Minute, MaxBackoff: 7 * 24 * time.Hour,
		MaxPause: 6 * time.Hour, MaxAttempts: 5, wake: make(chan struct{}, 1)}
}

// Wake tells the worker which song is playing now: it is queued (when it has
// lyrics) and translated before anything else. Never blocks; repeated calls
// for the same song are free.
func (w *Worker) Wake(trackID string) {
	if trackID == "" {
		return
	}
	w.mu.Lock()
	same := w.priority == trackID
	w.priority = trackID
	w.mu.Unlock()
	if !same {
		w.Nudge()
	}
}

// Nudge starts the next batch now (for example after lyrics were found).
// Never blocks.
func (w *Worker) Nudge() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run processes batches until ctx is done. A wake starts the next batch
// early, except during a pause (rate limit, cap reached).
func (w *Worker) Run(ctx context.Context) error {
	for {
		pause := w.RunOnce(ctx)
		wake := w.wake
		d := w.Interval
		if pause > 0 {
			d, wake = max(pause, w.Interval), nil
		}
		if !w.wait(ctx, d, wake) {
			return nil
		}
	}
}

// RunOnce queues the song playing now and recent plays, then translates up
// to Batch songs, claiming one at a time. It returns how long to pause
// before the next batch (0 = the normal interval).
func (w *Worker) RunOnce(ctx context.Context) time.Duration {
	prio := w.enqueuePriority(ctx)
	if w.RecentWindow > 0 {
		if _, err := w.Store.EnqueueRecentTranslations(ctx, w.RecentWindow); err != nil && ctx.Err() == nil {
			w.Log.WarnContext(ctx, "enqueue recent translations failed", logging.Err(err))
		}
	}
	var s stats
	defer s.log(ctx, w.Log)
	transient := 0
	called := false // a Claude call was made in this batch
	for i := 0; i < w.Batch; i++ {
		if i > 0 {
			prio = w.enqueuePriority(ctx) // a new song may have started meanwhile
		}
		cands, err := w.Store.ClaimTranslations(ctx, w.Owner, prio, w.Lease, 1)
		if err != nil {
			if ctx.Err() == nil {
				w.Log.WarnContext(ctx, "claim translation failed", logging.Err(err))
			}
			return 0
		}
		if len(cands) == 0 {
			break
		}
		// Space Claude calls; the lease (minutes) outlasts the wait.
		if called && !w.wait(ctx, w.Spacing, nil) {
			w.release(cands[0])
			return 0
		}
		pause, outcome := w.process(ctx, cands[0], &s)
		called = called || outcome != outcomeSkipped
		if pause > 0 || ctx.Err() != nil {
			return pause
		}
		switch outcome {
		case outcomeTransient:
			if transient++; transient >= maxTransient {
				return w.badBatchPause()
			}
		case outcomeDone:
			transient, w.badBatches = 0, 0
		default:
			transient = 0
		}
	}
	return 0
}

// enqueuePriority queues the song playing now once and returns its id.
func (w *Worker) enqueuePriority(ctx context.Context) string {
	w.mu.Lock()
	prio, done := w.priority, w.priority == w.enqueued
	w.mu.Unlock()
	if prio == "" || done {
		return prio
	}
	if _, err := w.Store.EnqueueTranslation(ctx, prio); err != nil {
		if ctx.Err() == nil {
			w.Log.WarnContext(ctx, "enqueue translation failed", slog.String("track_id", prio), logging.Err(err))
		}
		return prio
	}
	w.mu.Lock()
	w.enqueued = prio
	w.mu.Unlock()
	return prio
}

type outcome int

const (
	outcomeDone outcome = iota
	outcomeSkipped
	outcomeFailed // not transient (refused, permanent, invalid output)
	outcomeTransient
	outcomeNone // released without an outcome
)

// process translates one claimed song and records the outcome. It returns a
// pause when the worker must stop for a while (the claim is released).
func (w *Worker) process(ctx context.Context, c domain.TranslationCandidate, s *stats) (time.Duration, outcome) {
	req, ok := buildRequest(c)
	if !ok {
		s.skipped++
		w.deferTrack(ctx, c, domain.TranslationSkipped, nil, "nothing to translate or too long")
		return 0, outcomeSkipped
	}
	now := w.clock()
	okCall, err := w.Store.ReserveTranslationCall(ctx, now, w.DailyCap, w.MonthlyCap)
	if err != nil || !okCall {
		w.release(c)
		if err != nil {
			if ctx.Err() == nil {
				w.Log.WarnContext(ctx, "reserve translation call failed", logging.Err(err))
			}
			return w.Interval, outcomeNone
		}
		w.Log.InfoContext(ctx, "translation cap reached")
		return w.capPause(now), outcomeNone
	}
	res, err := w.Translator.Translate(ctx, req)
	if ctx.Err() != nil {
		w.release(c)
		return 0, outcomeNone
	}
	var ae *APIError
	var re *RefusalError
	switch {
	case errors.As(err, &ae) && ae.RateLimited():
		w.release(c)
		w.Log.WarnContext(ctx, "translation rate limited", slog.Duration("retry_after", ae.RetryAfter))
		return max(ae.RetryAfter, time.Minute), outcomeNone
	case errors.As(err, &ae) && (ae.SpendLimit() || ae.Auth()):
		w.release(c)
		w.Log.ErrorContext(ctx, "translation stopped: check the Anthropic API key and credit", slog.Int("status", ae.Status))
		return w.MaxPause, outcomeNone
	case errors.As(err, &re):
		s.refused++
		w.Log.WarnContext(ctx, "translation refused", slog.String("track_id", c.TrackID), slog.String("category", re.Category))
		w.deferTrack(ctx, c, domain.TranslationRefused, nil, err.Error())
		return 0, outcomeFailed
	case err != nil:
		s.failed++
		w.Log.WarnContext(ctx, "translation failed", slog.String("track_id", c.TrackID), logging.Err(err))
		next := w.retryAt(c)
		w.deferTrack(ctx, c, domain.TranslationError, next, err.Error())
		if (errors.As(err, &ae) && ae.Permanent()) || isOutputError(err) {
			return 0, outcomeFailed
		}
		return 0, outcomeTransient
	}
	s.tokensIn += res.InputTokens
	s.tokensOut += res.OutputTokens
	if err := w.Store.AddTranslationTokens(ctx, now, res.InputTokens, res.OutputTokens); err != nil && ctx.Err() == nil {
		w.Log.WarnContext(ctx, "record translation tokens failed", logging.Err(err))
	}
	tr := domain.Translation{Kind: req.Kind, SourceLanguage: res.SourceLanguage, EN: res.EN, VI: res.VI,
		ENText: res.ENText, VIText: res.VIText, Model: res.Model,
		InputTokens: res.InputTokens, OutputTokens: res.OutputTokens}
	stored, err := w.Store.SetTranslation(ctx, w.Owner, c.TrackID, c.SourceHash, tr)
	switch {
	case err != nil:
		s.failed++
		w.Log.WarnContext(ctx, "store translation failed", slog.String("track_id", c.TrackID), logging.Err(err))
		w.deferTrack(ctx, c, domain.TranslationError, w.retryAt(c), "store translation failed")
		return 0, outcomeFailed
	case !stored:
		// The lease expired or the lyrics changed meanwhile: the queue
		// picks the song up again.
		w.Log.InfoContext(ctx, "translation not stored (claim lost or lyrics changed)", slog.String("track_id", c.TrackID))
		w.release(c)
		return 0, outcomeNone
	}
	s.done++
	return 0, outcomeDone
}

func isOutputError(err error) bool {
	var ie *InvalidOutputError
	return errors.Is(err, ErrTruncated) || errors.As(err, &ie)
}

// buildRequest turns a candidate into a request: parsed synced lines when
// there are any (the same lines the public endpoint serves), else the plain
// text. ok is false when there is nothing to translate or it is too long.
func buildRequest(c domain.TranslationCandidate) (Request, bool) {
	r := Request{TrackID: c.TrackID, Title: c.Name, Artist: c.Artist}
	if lines := lrc.Parse(c.Synced); len(lines) > 0 {
		lines = lines[:min(len(lines), maxRequestLines)]
		r.Kind = domain.TranslationSynced
		r.Lines = make([]string, len(lines))
		n, runes := 0, 0
		for i, l := range lines {
			r.Lines[i] = lrc.CutRunes(l.Text, maxRequestRunes)
			if t := strings.TrimSpace(r.Lines[i]); t != "" {
				n++
				runes += utf8.RuneCountInString(t)
			}
		}
		return r, n > 0 && n <= maxSendLines && runes <= maxSendRunes
	}
	r.Kind = domain.TranslationPlain
	r.Plain = strings.TrimSpace(c.Plain)
	n := utf8.RuneCountInString(r.Plain)
	return r, n > 0 && n <= maxSendRunes
}

// retryAt is when a failed song is tried again, or nil to give up after
// MaxAttempts failures in a row.
func (w *Worker) retryAt(c domain.TranslationCandidate) *time.Time {
	n := int32(1)
	if c.Status == domain.TranslationError {
		n = c.Attempts + 1
	}
	if n >= w.MaxAttempts {
		return nil
	}
	t := w.clock().Add(backoff(n, w.MinBackoff, w.MaxBackoff))
	return &t
}

// capPause waits until the next UTC day, at most MaxPause; the cap is then
// checked again (a reached monthly cap keeps pausing until the month ends).
func (w *Worker) capPause(now time.Time) time.Duration {
	u := now.UTC()
	next := u.Truncate(24 * time.Hour).Add(24 * time.Hour)
	return min(max(next.Sub(u), time.Minute), w.MaxPause)
}

func (w *Worker) badBatchPause() time.Duration {
	w.badBatches++
	d := w.Interval
	for i := 0; i < w.badBatches && d < w.MaxPause; i++ {
		d *= 2
	}
	return min(d, w.MaxPause)
}

func (w *Worker) deferTrack(ctx context.Context, c domain.TranslationCandidate, status string, next *time.Time, msg string) {
	if len(msg) > maxErrLen {
		msg = strings.ToValidUTF8(msg[:maxErrLen], "")
	}
	if err := w.Store.DeferTranslation(ctx, w.Owner, c.TrackID, c.SourceHash, status, next, msg); err != nil {
		w.Log.WarnContext(ctx, "defer translation failed", slog.String("track_id", c.TrackID), logging.Err(err))
	}
}

// release gives the lease back, even when ctx is already cancelled.
func (w *Worker) release(c domain.TranslationCandidate) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Store.ReleaseTranslation(ctx, w.Owner, c.TrackID); err != nil {
		w.Log.Warn("release translation failed", slog.String("track_id", c.TrackID), logging.Err(err))
	}
}

// backoff is the delay after the attempt-th failure: lo doubled per earlier
// failure, at most hi.
func backoff(attempt int32, lo, hi time.Duration) time.Duration {
	d := lo
	for i := int32(1); i < attempt; i++ {
		if d >= hi/2 {
			return hi
		}
		d *= 2
	}
	return min(d, hi)
}

func (w *Worker) clock() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

// wait sleeps for d; a signal on wake (nil = none) ends it early. It returns
// false when ctx is done.
func (w *Worker) wait(ctx context.Context, d time.Duration, wake <-chan struct{}) bool {
	if w.sleep != nil {
		return w.sleep(ctx, d, wake)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	case <-wake:
		return true
	}
}

type stats struct {
	done, skipped, refused, failed int
	tokensIn, tokensOut            int64
}

func (s *stats) log(ctx context.Context, log *slog.Logger) {
	if s.done+s.skipped+s.refused+s.failed == 0 {
		return
	}
	log.InfoContext(ctx, "lyrics translated", slog.Int("done", s.done), slog.Int("skipped", s.skipped),
		slog.Int("refused", s.refused), slog.Int("errors", s.failed),
		slog.Int64("input_tokens", s.tokensIn), slog.Int64("output_tokens", s.tokensOut))
}
