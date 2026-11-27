package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/logging"
)

// pruneDaily deletes analytics older than 90 days and runs extra (nil for
// none), once a day.
func pruneDaily(ctx context.Context, recorder *analytics.Recorder, extra func(context.Context), log *slog.Logger) error {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := recorder.Prune(ctx, 90*24*time.Hour); err != nil {
				log.Warn("prune analytics failed", logging.Err(err))
			}
			if extra != nil {
				extra(ctx)
			}
		}
	}
}
