// Package lyrics fills in track lyrics from LRCLIB in the background. The
// queue is the lyrics table (store.ClaimLyrics): the song playing now, then
// the upcoming songs, then admin re-queues, then by last play, then the rest
// of the library; retries of tracks whose lookup found nothing or failed
// come back when due. Each track is claimed with a lease, so API replicas
// never look up the same track. Lyrics are shown in the admin dashboard, the
// admin MCP tools and the public now-playing lyrics.
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

// Store is the queue the refresher needs (implemented by *store.Queries).
type Store interface {
	ClaimLyrics(ctx context.Context, owner string, priority []string, lease time.Duration, limit int) ([]domain.LyricsCandidate, error)
	SetLyrics(ctx context.Context, owner, trackID string, l domain.Lyrics) (stored, retranslate bool, err error)
	DeferLyrics(ctx context.Context, owner, trackID, status string, next time.Time, lastErr string) error
	ReleaseLyrics(ctx context.Context, owner, trackID string) error
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

// Refresher periodically claims a batch of tracks to look up: tracks with no
// lyrics yet, re-queued tracks, and lookups due for a retry. The LRCLIB
// client spaces out requests; the refresher only paces batches.
type Refresher struct {
	Store         Store
	Source        Source
	Log           *slog.Logger
	Owner         string        // lease owner, unique per process
	Lease         time.Duration // claim length; an expired claim can be taken again
	Interval      time.Duration // between batches
	Batch         int
	NotFoundRetry time.Duration // re-check of a track with no lyrics found
	MinBackoff    time.Duration // first retry after a transient failure
	MaxBackoff    time.Duration // longest retry delay (also used for permanent errors)
	// MaxPause caps the growing pause after batches that end on transient
	// errors (Interval doubled per bad batch in a row).
	MaxPause time.Duration
	// OnFound, when set, is called after lyrics were found and stored for a
	// track (the translator uses it to start sooner, also when changed
	// lyrics re-queued the translation).
	OnFound func(trackID string)

	wake       chan struct{}
	mu         sync.Mutex
	priority   string    // the song playing now: looked up first
	upcoming   []string  // the next songs of the queue: looked up right after
	upcomingAt time.Time // when upcoming was set; it expires after UpcomingTTL
	gen        uint64    // bumped by a new song or a Nudge: a running batch stops
	badBatches int       // batches in a row that ended on maxTransient errors
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration, wake <-chan struct{}) bool
}

// New returns a Refresher with production defaults: batches of 10 a minute
// leased for 15 minutes, not-found tracks re-checked after 30 days, failures
// retried after 1h, doubling up to 7 days.
func New(st Store, src Source, owner string, log *slog.Logger) *Refresher {
	return &Refresher{Store: st, Source: src, Log: log, Owner: owner, Lease: 15 * time.Minute,
		Interval: time.Minute, Batch: 10, NotFoundRetry: 30 * 24 * time.Hour, MinBackoff: time.Hour,
		MaxBackoff: 7 * 24 * time.Hour, MaxPause: time.Hour, wake: make(chan struct{}, 1)}
}

// Wake tells the refresher which song is playing now: it is looked up before
// anything else. When the song changed, a running batch stops (its other
// claims are released) and the next batch starts now. Never blocks.
func (r *Refresher) Wake(trackID string) {
	if trackID == "" {
		return
	}
	r.mu.Lock()
	same := r.priority == trackID
	r.priority = trackID
	r.upcoming = slices.DeleteFunc(r.upcoming, func(id string) bool { return id == trackID }) // it plays now
	if !same {
		r.gen++
	}
	r.mu.Unlock()
	if !same {
		r.signal()
	}
}

// Nudge starts a new batch now (an admin re-queued a track): a running batch
// stops and releases its other claims. Never blocks.
func (r *Refresher) Nudge() {
	r.mu.Lock()
	r.gen++
	r.mu.Unlock()
	r.signal()
}

// signal starts the next batch early. Never blocks.
func (r *Refresher) signal() {
	if r.wake == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Refresher) generation() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gen
}

// UpcomingTTL is how long an upcoming list counts without a refresh (the
// poller re-reads the queue about every minute while a song plays).
const UpcomingTTL = 15 * time.Minute

// WakeUpcoming tells the refresher which songs are queued to play next (from
// the Spotify queue): they are looked up right after the song playing now,
// and a changed list starts the next batch now (a running batch finishes).
// An empty list clears it (playback went idle). Never blocks.
func (r *Refresher) WakeUpcoming(trackIDs []string) {
	r.mu.Lock()
	changed := !slices.Equal(r.upcoming, trackIDs)
	r.upcoming = slices.Clone(trackIDs)
	r.upcomingAt = r.clock()
	r.mu.Unlock()
	if changed && len(trackIDs) > 0 {
		r.signal()
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

// Run processes batches until ctx is done. A wake starts the next batch
// early, except during a pause (rate limit or bad batches): then it waits at
// least Interval, and neither Wake nor Nudge cuts the pause short.
func (r *Refresher) Run(ctx context.Context) error {
	for {
		wait, wake := r.Interval, r.wake
		if pause := r.RunOnce(ctx); pause > 0 {
			wait, wake = max(pause, r.Interval), nil
		}
		if !r.wait(ctx, wait, wake) {
			return nil
		}
	}
}

// RunOnce claims and processes one batch. It returns how long to pause
// before the next batch when LRCLIB rate-limited the refresher (Retry-After)
// or the batch ended on maxTransient errors in a row (Interval doubled per
// such batch in a row, at most MaxPause), otherwise 0. Claims it does not
// finish (shutdown, rate limit, a new song or a re-queue) are released.
func (r *Refresher) RunOnce(ctx context.Context) time.Duration {
	gen := r.generation()
	cands, err := r.Store.ClaimLyrics(ctx, r.Owner, r.priorityList(), r.Lease, r.Batch)
	if err != nil {
		if ctx.Err() == nil {
			r.Log.WarnContext(ctx, "claim lyrics lookups failed", logging.Err(err))
		}
		return 0
	}
	var s stats
	defer s.log(ctx, r.Log)
	transient := 0
	for i, c := range cands {
		if ctx.Err() != nil || r.generation() != gen {
			// Shutdown, or a new song or a re-queue goes first: the next
			// batch claims again in the new order.
			r.release(cands[i:])
			return 0
		}
		l, err := r.Source.Lookup(ctx, c)
		if ctx.Err() != nil {
			r.release(cands[i:])
			return 0
		}
		var rl *lrclib.RateLimitError
		var se *lrclib.StatusError
		switch {
		case errors.As(err, &rl):
			r.Log.WarnContext(ctx, "lyrics lookup rate limited", slog.Duration("retry_after", rl.RetryAfter))
			r.release(cands[i:])
			return rl.RetryAfter
		case errors.As(err, &se) && se.Permanent():
			// LRCLIB refuses this request: park the track for MaxBackoff so
			// it does not block the queue. Not counted as transient.
			s.failed++
			r.Log.WarnContext(ctx, "lyrics lookup refused", slog.String("track_id", c.TrackID), logging.Err(err))
			r.deferTrack(ctx, c.TrackID, domain.LyricsError, r.clock().Add(r.MaxBackoff), err.Error())
			continue
		case err != nil:
			s.failed++
			r.Log.WarnContext(ctx, "lyrics lookup failed", slog.String("track_id", c.TrackID), logging.Err(err))
			r.deferTrack(ctx, c.TrackID, domain.LyricsError, r.retryAt(c), err.Error())
			if transient++; transient >= maxTransient {
				r.release(cands[i+1:])
				return r.badBatchPause()
			}
			continue
		}
		transient = 0
		if l == nil {
			s.notFound++
			r.deferTrack(ctx, c.TrackID, domain.LyricsNotFound, r.clock().Add(r.NotFoundRetry), "")
			continue
		}
		r.store(ctx, c, *l, &s)
	}
	if s.found+s.instrumental+s.notFound > 0 {
		r.badBatches = 0
	}
	return 0
}

// store saves a successful lookup of c while the claim is held.
func (r *Refresher) store(ctx context.Context, c domain.LyricsCandidate, l domain.Lyrics, s *stats) {
	stored, retranslate, err := r.Store.SetLyrics(ctx, r.Owner, c.TrackID, l)
	switch {
	case err != nil:
		// Park the track so a row the database refuses is not picked again
		// in every batch.
		s.failed++
		r.Log.WarnContext(ctx, "store lyrics failed", slog.String("track_id", c.TrackID), logging.Err(err))
		r.deferTrack(ctx, c.TrackID, domain.LyricsError, r.retryAt(c), "store lyrics: "+err.Error())
		return
	case !stored:
		// The lease expired and another worker took the track.
		r.Log.InfoContext(ctx, "lyrics not stored (claim lost)", slog.String("track_id", c.TrackID))
		return
	case l.Status == domain.LyricsInstrumental:
		s.instrumental++
		return
	}
	s.found++
	if retranslate {
		r.Log.InfoContext(ctx, "lyrics changed: translation re-queued", slog.String("track_id", c.TrackID))
	}
	if r.OnFound != nil {
		r.OnFound(c.TrackID)
	}
}

// release gives back the leases of tracks not processed, even when ctx is
// already cancelled.
func (r *Refresher) release(cands []domain.LyricsCandidate) {
	if len(cands) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, c := range cands {
		if err := r.Store.ReleaseLyrics(ctx, r.Owner, c.TrackID); err != nil {
			r.Log.Warn("release lyrics failed", slog.String("track_id", c.TrackID), logging.Err(err))
		}
	}
}

// retryAt is when a failed lookup of c is retried: MinBackoff after the first
// error, doubling per earlier error (a not-found record restarts the count;
// for stored lyrics attempts counts failed re-lookups).
func (r *Refresher) retryAt(c domain.LyricsCandidate) time.Time {
	n := int32(1)
	switch c.Status {
	case domain.LyricsError, domain.LyricsFound, domain.LyricsInstrumental:
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
	if err := r.Store.DeferLyrics(ctx, r.Owner, id, status, next, msg); err != nil {
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
		return r.sleep(ctx, d, wake)
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
	found, instrumental, notFound, failed int
}

func (s *stats) log(ctx context.Context, log *slog.Logger) {
	if n := s.found + s.instrumental + s.notFound + s.failed; n > 0 {
		log.InfoContext(ctx, "lyrics refreshed", slog.Int("looked_up", n), slog.Int("found", s.found),
			slog.Int("instrumental", s.instrumental), slog.Int("not_found", s.notFound), slog.Int("errors", s.failed))
	}
}
