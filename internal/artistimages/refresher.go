// Package artistimages fills in artist profile images and genres in the
// background: new artists (from playback or imports) and a slow re-check of
// old ones.
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
	ArtistsNeedingProfile(ctx context.Context, staleBefore time.Time, limit int) ([]string, error)
	SetArtistProfile(ctx context.Context, id, imageURL string, genres []string) error
}

// Fetcher looks up an artist's profile (implemented by *spotify.Manager).
type Fetcher interface {
	ArtistProfile(ctx context.Context, id string) (spotify.ArtistProfile, error)
}

// maxTransient is how many transient failures in a row end a batch.
const maxTransient = 3

// Refresher periodically looks up images and genres for artists that were
// never looked up or whose lookup is older than MaxAge. Requests are spaced out so a large
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
	wake  chan struct{}
}

// New returns a Refresher with production defaults: up to 30 lookups a
// minute, profiles re-checked every 30 days.
func New(st Store, f Fetcher, log *slog.Logger) *Refresher {
	return &Refresher{Store: st, Fetcher: f, Log: log, Interval: time.Minute,
		Spacing: 2 * time.Second, Batch: 25, MaxAge: 30 * 24 * time.Hour, wake: make(chan struct{}, 1)}
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
	ids, err := r.Store.ArtistsNeedingProfile(ctx, time.Now().Add(-r.MaxAge), r.Batch)
	if err != nil {
		r.Log.WarnContext(ctx, "list artists needing profile failed", logging.Err(err))
		return 0
	}
	stored, transient := 0, 0
	for i, id := range ids {
		if i > 0 && !r.wait(ctx, r.Spacing) {
			return 0
		}
		p, err := r.Fetcher.ArtistProfile(ctx, id)
		var rl *spotify.RateLimitError
		var se *spotify.StatusError
		switch {
		case errors.As(err, &rl):
			r.Log.WarnContext(ctx, "artist profile rate limited", slog.Duration("retry_after", rl.RetryAfter))
			return rl.RetryAfter
		case errors.Is(err, spotify.ErrNotConnected) || errors.Is(err, spotify.ErrInvalidGrant):
			// Waiting for (re-)authorization; alerts cover this already.
			r.Log.DebugContext(ctx, "artist profile paused: spotify not authorized", logging.Err(err))
			return 0
		case errors.As(err, &se) && se.Permanent():
			// Spotify refuses this artist for good (e.g. 400 for a bad ID):
			// record the lookup without data so it does not block the rest
			// of the queue; it is re-checked after MaxAge.
			r.Log.WarnContext(ctx, "artist profile lookup refused; recorded without data",
				slog.String("artist_id", id), logging.Err(err))
			p = spotify.ArtistProfile{}
		case err != nil:
			// Transient failure (5xx, network): skip this artist (it is
			// retried next batch) unless too many fail in a row.
			r.Log.WarnContext(ctx, "fetch artist profile failed", slog.String("artist_id", id), logging.Err(err))
			if transient++; transient >= maxTransient {
				return 0
			}
			continue
		}
		transient = 0
		// A refused lookup stores nil genres, which keeps any stored ones.
		var genres []string
		if p.Found {
			genres = p.Genres
			if genres == nil {
				genres = []string{}
			}
		}
		if err := r.Store.SetArtistProfile(ctx, id, p.ImageURL, genres); err != nil {
			r.Log.WarnContext(ctx, "store artist profile failed", slog.String("artist_id", id), logging.Err(err))
			continue
		}
		if p.ImageURL != "" {
			stored++
		}
	}
	if len(ids) > 0 {
		r.Log.InfoContext(ctx, "artist profiles refreshed", slog.Int("looked_up", len(ids)), slog.Int("with_image", stored))
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
