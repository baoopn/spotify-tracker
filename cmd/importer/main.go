// Command importer copies recorded data from the legacy Directus instance
// into PostgreSQL. Safe to re-run.
//
//	importer                 # enriched features + their catalog (default; no history)
//	importer -mode=all       # every collection, including listening history
//	importer -dry-run        # read from Directus only, write nothing
package main

import (
	// Embedded IANA tz database: zone and DST rules come from the Go
	// toolchain, not from the base image.
	_ "time/tzdata"

	"context"
	"flag"
	"fmt"
	"log/slog"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/importer"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/app"
	"github.com/baoopn/spotify-tracker/internal/platform/postgres"
	"github.com/baoopn/spotify-tracker/internal/store"
)

func main() { app.Main(run) }

func run(ctx context.Context) error {
	dryRun := flag.Bool("dry-run", false, "read from Directus without writing to Postgres")
	mode := flag.String("mode", "enriched",
		"enriched: only enriched audio features + their tracks, albums and artists (no history); "+
			"all: every collection including listening history")
	flag.Parse()
	if *mode != "enriched" && *mode != "all" {
		return fmt.Errorf("unknown -mode %q (use enriched or all)", *mode)
	}

	cfg, err := config.Load[config.Importer]()
	if err != nil {
		return err
	}
	log := logging.New("importer", cfg.LogLevel, cfg.LogFormat)

	pool, err := postgres.Connect(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool, log); err != nil {
		return err
	}

	im := importer.New(cfg.DirectusURL, cfg.DirectusToken, cfg.PageSize, store.New(pool), log, *dryRun)
	if *mode == "enriched" {
		st, err := im.RunEnriched(ctx)
		log.Info("enriched import finished", slog.Bool("dry_run", *dryRun),
			slog.Int("features_found", st.FeaturesFound), slog.Int("features_imported", st.FeaturesImported),
			slog.Int("features_kept_existing", st.FeaturesKept), slog.Int("orphans", st.Orphans),
			slog.Int("tracks", st.Tracks), slog.Int("albums", st.Albums), slog.Int("artists", st.Artists))
		return err
	}

	stats, err := im.Run(ctx)
	log.Info("import finished",
		slog.Bool("dry_run", *dryRun),
		slog.Int("artists", stats.Artists), slog.Int("albums", stats.Albums),
		slog.Int("tracks", stats.Tracks), slog.Int("track_features", stats.Features),
		slog.Int("history_created", stats.History), slog.Int("history_skipped", stats.HistorySkipped),
		slog.Bool("current_playback", stats.CurrentPlayback))
	return err
}
