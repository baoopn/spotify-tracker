// Package lyrics fills in track lyrics from LRCLIB in the background: new
// tracks first (most recently played first), then retries of tracks whose
// lookup found nothing or failed. Lyrics are shown in the admin dashboard and
// the admin MCP tools only.
package lyrics

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/lrclib"
)

// Store is the persistence the refresher needs (implemented by *store.Queries).
type Store interface {
	TracksNeedingLyrics(ctx context.Context, limit int, priority []string) ([]domain.LyricsCandidate, error)
	SetLyrics(ctx context.Context, trackID string, l domain.Lyrics) error
	DeferLyrics(ctx context.Context, trackID, status string, next time.Time, lastErr string) error
}

// Source looks up lyrics (implemented by *lrclib.Client). nil, nil means
// no lyrics were found.
type Source interface {
	Lookup(ctx context.Context, c domain.LyricsCandidate) (*domain.Lyrics, error)
}

// maxTransient is how many transient failures in a row end a batch.
const maxTransient = 3

// maxErrLen caps the stored last_error.
const maxErrLen = 300

// Refresher periodically looks up lyrics for tracks that have none yet, or
// whose earlier lookup is due for a retry. The LRCLIB client spaces out
// requests; the refresher only paces batches.
type Refresher struct {
	Store         Store
	Source        Source
	Log           *slog.Logger
	Interval      time.Duration // between batches
	Batch         int
	NotFoundRetry time.Duration // re-check of a track with no lyrics found
	MinBackoff    time.Duration // first retry after a transient failure
	MaxBackoff    time.Duration // longest retry delay (also used for permanent errors)
	// MaxPause caps the growing pause after batches that end on transient
	// errors (Interval doubled per bad batch in a row).
	MaxPause time.Duration
	// OnFound, when set, is called after lyrics were found and stored for a
	// track (the translator uses it to start sooner).
	OnFound func(trackID string)

	wake       chan struct{}
	mu         sync.Mutex
	priority   string    // the song playing now: looked up first
	upcoming   []string  // the next songs of the queue: looked up right after
	upcomingAt time.Time // when upcoming was set; it expires after UpcomingTTL
	badBatches int       // batches in a row that ended on maxTransient errors
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration) bool
}

// New returns a Refresher with production defaults: batches of 10 a minute,
// not-found tracks re-checked after 30 days, failures retried after 1h,
// doubling up to 7 days.
func New(st Store, src Source, log *slog.Logger) *Refresher {
	return &Refresher{Store: st, Source: src, Log: log, Interval: time.Minute, Batch: 10,
		NotFoundRetry: 30 * 24 * time.Hour, MinBackoff: time.Hour, MaxBackoff: 7 * 24 * time.Hour,
		MaxPause: time.Hour, wake: make(chan struct{}, 1)}
}

// Wake tells the refresher which song is playing now: it is looked up before
// anything else, and the next batch starts now when the song changed. Never
// blocks.
func (r *Refresher) Wake(trackID string) {
	if trackID == "" {
		return
	}
	r.mu.Lock()
	same := r.priority == trackID
	r.priority = trackID
	r.upcoming = slices.DeleteFunc(r.upcoming, func(id string) bool { return id == trackID }) // it plays now
	r.mu.Unlock()
	if same || r.wake == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// UpcomingTTL is how long an upcoming list counts without a refresh (the
// poller re-reads the queue about every minute while a song plays).
const UpcomingTTL = 15 * time.Minute

// WakeUpcoming tells the refresher which songs are queued to play next (from
// the Spotify queue): they are looked up right after the song playing now,
// and a changed list starts the next batch now. An empty list clears it
// (playback went idle). Never blocks.
func (r *Refresher) WakeUpcoming(trackIDs []string) {
	r.mu.Lock()
	changed := !slices.Equal(r.upcoming, trackIDs)
	r.upcoming = slices.Clone(trackIDs)
	r.upcomingAt = r.clock()
	r.mu.Unlock()
	if !changed || len(trackIDs) == 0 || r.wake == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// priorityList is the song playing now, then the upcoming songs (unless the
// list expired).
func (r *Refresher) priorityList() []string {
	now := r.clock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if now.Sub(r.upcomingAt) > UpcomingTTL {
		r.upcoming = nil
	}
	out := make([]string, 0, 1+len(r.upcoming))
	if r.priority != "" {
		out = append(out, r.priority)
	}
	for _, id := range r.upcoming {
		if id != r.priority {
			out = append(out, id)
		}
	}
	return out
}

// Run processes batches until ctx is done.
func (r *Refresher) Run(ctx context.Context) error {
	for {
		wait, wake := r.Interval, r.wake
		if pause := r.RunOnce(ctx); pause > wait {
			wait, wake = pause, nil // a rate limit or bad batch pause is not cut short
		}
		if !r.wait(ctx, wait, wake) {
			return nil
		}
	}
}

// RunOnce processes one batch. It returns how long to pause before the next
// batch when LRCLIB rate-limited the refresher (Retry-After) or the batch
// ended on maxTransient errors in a row (Interval doubled per such batch in a
// row, at most MaxPause), otherwise 0.
func (r *Refresher) RunOnce(ctx context.Context) time.Duration {
	cands, err := r.Store.TracksNeedingLyrics(ctx, r.Batch, r.priorityList())
	if err != nil {
		if ctx.Err() == nil {
			r.Log.WarnContext(ctx, "list tracks needing lyrics failed", logging.Err(err))
		}
		return 0
	}
	var found, instrumental, notFound, failed, transient int
	defer func() {
		if n := found + instrumental + notFound + failed; n > 0 {
			r.Log.InfoContext(ctx, "lyrics refreshed", slog.Int("looked_up", n), slog.Int("found", found),
				slog.Int("instrumental", instrumental), slog.Int("not_found", notFound), slog.Int("errors", failed))
		}
	}()
	for _, c := range cands {
		l, err := r.Source.Lookup(ctx, c)
		if ctx.Err() != nil {
			return 0
		}
		var rl *lrclib.RateLimitError
		var se *lrclib.StatusError
		switch {
		case errors.As(err, &rl):
			r.Log.WarnContext(ctx, "lyrics lookup rate limited", slog.Duration("retry_after", rl.RetryAfter))
			return rl.RetryAfter
		case errors.As(err, &se) && se.Permanent():
			// LRCLIB refuses this request: park the track for MaxBackoff so
			// it does not block the queue. Not counted as transient.
			failed++
			r.Log.WarnContext(ctx, "lyrics lookup refused", slog.String("track_id", c.TrackID), logging.Err(err))
			r.deferTrack(ctx, c.TrackID, domain.LyricsError, r.clock().Add(r.MaxBackoff), err.Error())
			continue
		case err != nil:
			failed++
			r.Log.WarnContext(ctx, "lyrics lookup failed", slog.String("track_id", c.TrackID), logging.Err(err))
			r.deferTrack(ctx, c.TrackID, domain.LyricsError, r.retryAt(c), err.Error())
			if transient++; transient >= maxTransient {
				return r.badBatchPause()
			}
			continue
		}
		transient = 0
		if l == nil {
			notFound++
			r.deferTrack(ctx, c.TrackID, domain.LyricsNotFound, r.clock().Add(r.NotFoundRetry), "")
			continue
		}
		if err := r.Store.SetLyrics(ctx, c.TrackID, *l); err != nil {
			// Park the track so a row the database refuses is not picked
			// again in every batch.
			failed++
			r.Log.WarnContext(ctx, "store lyrics failed", slog.String("track_id", c.TrackID), logging.Err(err))
			r.deferTrack(ctx, c.TrackID, domain.LyricsError, r.retryAt(c), "store lyrics: "+err.Error())
			continue
		}
		if l.Status == domain.LyricsInstrumental {
			instrumental++
		} else {
			found++
			if r.OnFound != nil {
				r.OnFound(c.TrackID)
			}
		}
	}
	if found+instrumental+notFound > 0 {
		r.badBatches = 0
	}
	return 0
}

// retryAt is when a failed lookup of c is retried: MinBackoff after the first
// error, doubling per earlier error (a not-found record restarts the count).
func (r *Refresher) retryAt(c domain.LyricsCandidate) time.Time {
	n := int32(1)
	if c.Status == domain.LyricsError {
		n = c.Attempts + 1
	}
	return r.clock().Add(backoff(n, r.MinBackoff, r.MaxBackoff))
}

// badBatchPause records a batch that ended on transient errors and returns
// the pause before the next one.
func (r *Refresher) badBatchPause() time.Duration {
	r.badBatches++
	d := r.Interval
	for i := 0; i < r.badBatches && d < r.MaxPause; i++ {
		d *= 2
	}
	return min(d, r.MaxPause)
}

func (r *Refresher) deferTrack(ctx context.Context, id, status string, next time.Time, msg string) {
	if len(msg) > maxErrLen {
		msg = strings.ToValidUTF8(msg[:maxErrLen], "") // never split a rune
	}
	if err := r.Store.DeferLyrics(ctx, id, status, next, msg); err != nil {
		r.Log.WarnContext(ctx, "defer lyrics failed", slog.String("track_id", id), logging.Err(err))
	}
}

// backoff is the retry delay after the attempt-th failure: lo doubled per
// earlier failure, at most hi.
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

func (r *Refresher) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// wait sleeps for d; a signal on wake (nil = none) ends it early.
func (r *Refresher) wait(ctx context.Context, d time.Duration, wake <-chan struct{}) bool {
	if r.sleep != nil {
		return r.sleep(ctx, d)
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
