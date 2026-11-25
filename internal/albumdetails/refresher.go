// Package albumdetails fills in album details (type, size, release date
// precision, UPC, copyrights and track numbers) in the background: new albums
// (from playback or imports) and a slow re-check of old ones.
package albumdetails

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// Store is the persistence the refresher needs (implemented by *store.Queries).
type Store interface {
	AlbumsNeedingDetails(ctx context.Context, staleBefore time.Time, limit int) ([]string, error)
	SetAlbumDetails(ctx context.Context, id string, d *domain.AlbumDetails) error
	DeferAlbumDetails(ctx context.Context, id string, age time.Duration) error
}

// Fetcher looks up an album (implemented by *spotify.Manager).
type Fetcher interface {
	AlbumDetails(ctx context.Context, id string) (*domain.AlbumDetails, error)
}

// maxTransient is how many transient failures in a row end a batch.
const maxTransient = 3

// Refresher periodically looks up details for albums that were never looked
// up or whose lookup is older than MaxAge. Requests are spaced out so a large
// backfill stays well inside Spotify's per-account quota.
type Refresher struct {
	Store    Store
	Fetcher  Fetcher
	Log      *slog.Logger
	Interval time.Duration // between batches
	Spacing  time.Duration // between requests within a batch
	Batch    int
	MaxAge   time.Duration
	RetryIn  time.Duration // after a transient failure

	sleep func(ctx context.Context, d time.Duration) bool
	wake  chan struct{}
}

// New returns a Refresher with production defaults: up to 15 lookups a
// minute, details re-checked every 180 days, a failed lookup retried after
// an hour.
func New(st Store, f Fetcher, log *slog.Logger) *Refresher {
	return &Refresher{Store: st, Fetcher: f, Log: log, Interval: time.Minute,
		Spacing: 2 * time.Second, Batch: 15, MaxAge: 180 * 24 * time.Hour, RetryIn: time.Hour, wake: make(chan struct{}, 1)}
}

// Wake starts the next batch now (queue prefetch: an upcoming track may
// bring a new album or artist). A rate-limit pause is not cut short. Never
// blocks.
func (r *Refresher) Wake() {
	if r.wake == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run processes batches until ctx is done.
func (r *Refresher) Run(ctx context.Context) error {
	for {
		wait, wake := r.Interval, r.wake
		if pause := r.RunOnce(ctx); pause > wait {
			wait, wake = pause, nil // a rate-limit pause is not cut short
		}
		if !r.waitWake(ctx, wait, wake) {
			return nil
		}
	}
}

// RunOnce processes one batch. It returns how long to pause before the next
// batch when Spotify rate-limited the refresher, otherwise 0.
func (r *Refresher) RunOnce(ctx context.Context) time.Duration {
	ids, err := r.Store.AlbumsNeedingDetails(ctx, time.Now().Add(-r.MaxAge), r.Batch)
	if err != nil {
		r.Log.WarnContext(ctx, "list albums needing details failed", logging.Err(err))
		return 0
	}
	found, transient := 0, 0
	for i, id := range ids {
		if i > 0 && !r.wait(ctx, r.Spacing) {
			return 0
		}
		d, err := r.Fetcher.AlbumDetails(ctx, id)
		var rl *spotify.RateLimitError
		var se *spotify.StatusError
		switch {
		case errors.As(err, &rl):
			r.Log.WarnContext(ctx, "album details rate limited", slog.Duration("retry_after", rl.RetryAfter))
			return rl.RetryAfter
		case errors.Is(err, spotify.ErrNotConnected) || errors.Is(err, spotify.ErrInvalidGrant):
			// Waiting for (re-)authorization; alerts cover this already.
			r.Log.DebugContext(ctx, "album details paused: spotify not authorized", logging.Err(err))
			return 0
		case errors.As(err, &se) && se.Permanent():
			// Spotify refuses this album for good: record the lookup without
			// data so it does not block the queue; re-checked after MaxAge.
			r.Log.WarnContext(ctx, "album details lookup refused; recorded without data",
				slog.String("album_id", id), logging.Err(err))
			d = nil
		case err != nil:
			// Transient failure (5xx, network, decode, repeated 401): retry
			// this album after RetryIn so it does not stay at the head of
			// the queue, and go on with the next one.
			r.Log.WarnContext(ctx, "fetch album details failed", slog.String("album_id", id), logging.Err(err))
			if err := r.Store.DeferAlbumDetails(ctx, id, r.MaxAge-r.RetryIn); err != nil {
				r.Log.WarnContext(ctx, "defer album details failed", slog.String("album_id", id), logging.Err(err))
			}
			if transient++; transient >= maxTransient {
				return 0
			}
			continue
		}
		transient = 0
		if err := r.Store.SetAlbumDetails(ctx, id, d); err != nil {
			r.Log.WarnContext(ctx, "store album details failed", slog.String("album_id", id), logging.Err(err))
			continue
		}
		if d != nil {
			found++
		}
	}
	if len(ids) > 0 {
		r.Log.InfoContext(ctx, "album details refreshed", slog.Int("looked_up", len(ids)), slog.Int("found", found))
	}
	return 0
}

// waitWake is wait that a signal on wake (nil = none) ends early.
func (r *Refresher) waitWake(ctx context.Context, d time.Duration, wake <-chan struct{}) bool {
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

func (r *Refresher) wait(ctx context.Context, d time.Duration) bool {
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
	}
}
