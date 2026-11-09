// Package alerts runs the API's operator alert checks (Spotify refresh-token
// expiry) and sends them through the notifier.
package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/baoopn/spotify-tracker/internal/notify"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

// Checker periodically checks the Spotify token expiry.
type Checker struct {
	settings     *settings.Store
	notifier     *notify.Notifier
	lifetimeDays int
	reauthURL    string
	log          *slog.Logger
}

// New creates a Checker. reauthURL is the dashboard page used to re-authorize.
func New(st *settings.Store, n *notify.Notifier, lifetimeDays int, reauthURL string, log *slog.Logger) *Checker {
	return &Checker{settings: st, notifier: n, lifetimeDays: lifetimeDays, reauthURL: reauthURL, log: log}
}

// Run checks every interval until ctx is cancelled.
func (c *Checker) Run(ctx context.Context, interval time.Duration) error {
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		c.CheckExpiry(ctx)
		t.Reset(interval)
	}
}

// ExpiresAt returns when a token authorized at authorizedAt expires.
func ExpiresAt(authorizedAt time.Time, lifetimeDays int) time.Time {
	return authorizedAt.AddDate(0, 0, lifetimeDays)
}

// CheckExpiry sends an alert when the token enters an alert stage.
func (c *Checker) CheckExpiry(ctx context.Context) {
	sp, err := c.settings.Spotify(ctx)
	if errors.Is(err, settings.ErrNotFound) || (err == nil && sp.AuthorizedAt == nil) {
		return
	}
	if err != nil {
		c.log.WarnContext(ctx, "expiry check: load spotify settings failed", slog.Any("error", err))
		return
	}
	stages := settings.DefaultExpiryAlertDays
	if em, err := c.settings.Email(ctx); err == nil && len(em.ExpiryAlertDays) > 0 {
		stages = em.ExpiryAlertDays
	}

	exp := ExpiresAt(*sp.AuthorizedAt, c.lifetimeDays)
	left := time.Until(exp)
	stage, ok := Stage(left, stages)
	if !ok {
		return
	}
	if stage == 0 {
		c.log.ErrorContext(ctx, "spotify refresh token has expired; re-authorize",
			slog.String("expired_at", exp.UTC().Format(time.RFC3339)))
	} else {
		c.log.WarnContext(ctx, "spotify refresh token expires soon",
			slog.String("expires_at", exp.UTC().Format(time.RFC3339)), slog.Duration("remaining", left.Round(time.Hour)))
	}
	key := fmt.Sprintf("spotify-expiry:%s:%d", exp.UTC().Format("2006-01-02 15:04 UTC"), stage)
	c.notifier.Once(ctx, key, 30*24*time.Hour, ExpiryEmail(stage, exp, c.lifetimeDays, c.reauthURL, time.Now()))
}

// InvalidGrant sends (at most daily) an alert that Spotify rejected the token.
func (c *Checker) InvalidGrant(ctx context.Context, cause error) {
	c.notifier.Once(ctx, "spotify-auth-failed", 24*time.Hour, InvalidGrantEmail(cause, c.reauthURL))
}

// Stage maps the time left before expiry to an alert stage in days
// (0 = expired). stages are the configured days-before-expiry.
func Stage(left time.Duration, stages []int) (int, bool) {
	if left <= 0 {
		return 0, true
	}
	sorted := append([]int(nil), stages...)
	sort.Ints(sorted)
	for _, s := range sorted {
		if s > 0 && left <= time.Duration(s)*24*time.Hour {
			return s, true
		}
	}
	return 0, false
}
