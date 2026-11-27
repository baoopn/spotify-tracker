package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/logging"
)

// pruneFirstDelay is when the first prune runs after start: deploys restart
// the API, so a daily ticker alone might never fire.
const pruneFirstDelay = 5 * time.Minute

// pruneDaily deletes analytics older than 90 days and runs extra (nil for
// none): about 5 minutes after start, then once a day.
func pruneDaily(ctx context.Context, recorder *analytics.Recorder, extra func(context.Context), log *slog.Logger) error {
	t := time.NewTimer(pruneFirstDelay)
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
			t.Reset(24 * time.Hour)
		}
	}
}
