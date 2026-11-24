package poller

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/grpcapi"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/observability"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// QueueSource reads the user's Spotify queue (implemented by *spotify.Player).
type QueueSource interface {
	Queue(ctx context.Context, limit int) ([]domain.CatalogEntry, error)
}

// PrefetchConfig tunes queue prefetch.
type PrefetchConfig struct {
	Depth   int           // upcoming tracks to prefetch
	Recheck time.Duration // re-read the queue this often while one song plays
	TTL     time.Duration // a prefetched track is not prefetched again for this long
}

const (
	prefetchTimeout   = 10 * time.Second
	forbiddenBackoff  = time.Hour
	maxDoneEntries    = 500
	maxUpcomingListed = 20 // the RPC accepts at most this many
)

// prefetcher reads the queue in its own goroutine, so a slow queue
// response never delays the poll loop.
type prefetcher struct {
	src    QueueSource
	ingest trackerv1.IngestServiceClient
	cfg    PrefetchConfig
	log    *slog.Logger
	// pause makes the whole poller wait (Spotify rate limits are app wide).
	pause func(until time.Time)

	mu       sync.Mutex
	done     map[string]time.Time // track id -> prefetched at
	forTrack string               // the song the last read was for
	lastAt   time.Time
	retryAt  time.Time
	warned   bool // the 403 warning was logged

	running atomic.Bool
	now     func() time.Time
}

// EnablePrefetch turns on queue prefetch: while a song plays, the poller
// reads the queue when the song changes and every Recheck, and sends the
// next tracks to the API.
func (p *Poller) EnablePrefetch(src QueueSource, cfg PrefetchConfig) {
	cfg.Depth = max(1, min(cfg.Depth, maxUpcomingListed))
	p.prefetch = &prefetcher{
		src: src, ingest: p.ingest, cfg: cfg, log: p.log,
		pause: func(until time.Time) { p.pauseUntil.Store(until.UnixNano()) },
		done:  map[string]time.Time{}, now: time.Now,
	}
}

// due reports whether the queue should be read now for the song playing.
func (f *prefetcher) due(currentID string, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running.Load() || now.Before(f.retryAt) {
		return false
	}
	return currentID != f.forTrack || now.Sub(f.lastAt) >= f.cfg.Recheck
}

// idle forgets the song, so the next song reads the queue at once.
func (f *prefetcher) idle() {
	f.mu.Lock()
	f.forTrack = ""
	f.mu.Unlock()
}

// kick starts a queue read in the background when one is due. Only call it
// for a snapshot that is playing.
func (f *prefetcher) kick(ctx context.Context, currentID string) {
	now := f.now()
	if !f.due(currentID, now) || !f.running.CompareAndSwap(false, true) {
		return
	}
	f.mu.Lock()
	f.forTrack, f.lastAt = currentID, now
	f.mu.Unlock()
	go func() {
		defer f.running.Store(false)
		f.run(ctx, currentID)
	}()
}

// run reads the queue once and reports it to the API.
func (f *prefetcher) run(ctx context.Context, currentID string) {
	ctx, cancel := context.WithTimeout(ctx, prefetchTimeout)
	defer cancel()
	entries, err := f.src.Queue(ctx, f.cfg.Depth+1) // +1: the current song may be queued again
	if err != nil {
		f.fail(ctx, err)
		return
	}
	now := f.now()
	f.mu.Lock()
	f.prune(now)
	req := &trackerv1.PrefetchUpcomingRequest{CurrentTrackId: currentID, FetchedAt: timestamppb.New(now)}
	for _, e := range entries {
		if e.Track.ID == currentID {
			continue // repeat-one: it is playing already
		}
		if len(req.Tracks) >= f.cfg.Depth {
			break
		}
		_, recent := f.done[e.Track.ID]
		req.Tracks = append(req.Tracks, grpcapi.UpcomingToProto(domain.Upcoming{
			Position: int32(len(req.Tracks)), CatalogEntry: e, Prefetch: !recent,
		}))
	}
	f.mu.Unlock()
	if len(req.Tracks) == 0 {
		observability.QueueFetches.WithLabelValues("empty").Inc()
	} else {
		observability.QueueFetches.WithLabelValues("ok").Inc()
	}

	res, err := f.ingest.PrefetchUpcoming(ctx, req)
	if err != nil {
		if ctx.Err() == nil {
			f.log.WarnContext(ctx, "prefetch upcoming failed", logging.Err(err))
		}
		return // nothing marked done: the next read tries again
	}
	f.mu.Lock()
	for _, id := range res.GetPrefetchedTrackIds() {
		f.done[id] = now
	}
	f.mu.Unlock()
	if n := len(res.GetPrefetchedTrackIds()); n > 0 {
		observability.PrefetchedTracks.Add(float64(n))
		f.log.InfoContext(ctx, "upcoming tracks prefetched", slog.Int("count", n))
	}
}

// fail backs off after a failed queue read.
func (f *prefetcher) fail(ctx context.Context, err error) {
	now := f.now()
	var rl *spotify.RateLimitError
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case errors.As(err, &rl):
		observability.QueueFetches.WithLabelValues("rate_limited").Inc()
		f.retryAt = now.Add(max(rl.RetryAfter, f.cfg.Recheck))
		if f.pause != nil {
			f.pause(now.Add(rl.RetryAfter))
		}
		f.log.WarnContext(ctx, "spotify rate limited the queue read", slog.Duration("retry_after", rl.RetryAfter))
	case errors.Is(err, spotify.ErrQueueForbidden):
		observability.QueueFetches.WithLabelValues("forbidden").Inc()
		f.retryAt = now.Add(forbiddenBackoff)
		if !f.warned {
			f.warned = true
			f.log.WarnContext(ctx, "spotify refused the queue read (403); queue prefetch retries hourly")
		}
	default:
		if ctx.Err() != nil && errors.Is(err, context.Canceled) {
			return
		}
		observability.QueueFetches.WithLabelValues("error").Inc()
		f.retryAt = now.Add(f.cfg.Recheck)
		f.log.WarnContext(ctx, "read spotify queue failed", logging.Err(err))
	}
}

// prune drops expired entries and keeps the map bounded. Call with mu held.
func (f *prefetcher) prune(now time.Time) {
	for id, at := range f.done {
		if now.Sub(at) >= f.cfg.TTL {
			delete(f.done, id)
		}
	}
	for len(f.done) > maxDoneEntries {
		oldest, oldestAt := "", now
		for id, at := range f.done {
			if at.Before(oldestAt) || oldest == "" {
				oldest, oldestAt = id, at
			}
		}
		delete(f.done, oldest)
	}
}
