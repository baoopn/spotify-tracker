// Package artistimages fills in artist profile images in the background:
// new artists (from playback or imports) and a slow re-check of old ones.
package artistimages

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// Store is the persistence the refresher needs (implemented by *store.Queries).
type Store interface {
	ArtistsNeedingImage(ctx context.Context, staleBefore time.Time, limit int) ([]string, error)
	SetArtistImage(ctx context.Context, id, url string) error
}

// Fetcher looks up an artist's image (implemented by *spotify.Manager).
type Fetcher interface {
	ArtistImage(ctx context.Context, id string) (string, error)
}

// Refresher periodically looks up images for artists that have none yet or
// whose lookup is older than MaxAge. Requests are spaced out so a large
// backfill stays well inside Spotify's per-account quota.
type Refresher struct {
	Store    Store
	Fetcher  Fetcher
	Log      *slog.Logger
	Interval time.Duration // between batches
	Spacing  time.Duration // between requests within a batch
	Batch    int
	MaxAge   time.Duration

	sleep func(ctx context.Context, d time.Duration) bool
}

// New returns a Refresher with production defaults: up to 30 lookups a
// minute, images re-checked every 30 days.
func New(st Store, f Fetcher, log *slog.Logger) *Refresher {
	return &Refresher{Store: st, Fetcher: f, Log: log, Interval: time.Minute,
		Spacing: 2 * time.Second, Batch: 25, MaxAge: 30 * 24 * time.Hour}
}

// Run processes batches until ctx is done.
func (r *Refresher) Run(ctx context.Context) error {
	for {
		wait := r.Interval
		if pause := r.RunOnce(ctx); pause > wait {
			wait = pause
		}
		if !r.wait(ctx, wait) {
			return nil
		}
	}
}

// RunOnce processes one batch. It returns how long to pause before the next
// batch when Spotify rate-limited the refresher, otherwise 0.
func (r *Refresher) RunOnce(ctx context.Context) time.Duration {
	ids, err := r.Store.ArtistsNeedingImage(ctx, time.Now().Add(-r.MaxAge), r.Batch)
	if err != nil {
		r.Log.WarnContext(ctx, "list artists needing images failed", logging.Err(err))
		return 0
	}
	stored := 0
	for i, id := range ids {
		if i > 0 && !r.wait(ctx, r.Spacing) {
			return 0
		}
		img, err := r.Fetcher.ArtistImage(ctx, id)
		var rl *spotify.RateLimitError
		switch {
		case errors.As(err, &rl):
			r.Log.WarnContext(ctx, "artist images rate limited", slog.Duration("retry_after", rl.RetryAfter))
			return rl.RetryAfter
		case errors.Is(err, spotify.ErrNotConnected) || errors.Is(err, spotify.ErrInvalidGrant):
			// Waiting for (re-)authorization; alerts cover this already.
			r.Log.DebugContext(ctx, "artist images paused: spotify not authorized", logging.Err(err))
			return 0
		case err != nil:
			// Transient failure; try again next batch.
			r.Log.WarnContext(ctx, "fetch artist image failed", slog.String("artist_id", id), logging.Err(err))
			return 0
		}
		if err := r.Store.SetArtistImage(ctx, id, img); err != nil {
			r.Log.WarnContext(ctx, "store artist image failed", slog.String("artist_id", id), logging.Err(err))
			continue
		}
		if img != "" {
			stored++
		}
	}
	if len(ids) > 0 {
		r.Log.InfoContext(ctx, "artist images refreshed", slog.Int("looked_up", len(ids)), slog.Int("with_image", stored))
	}
	return 0
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
