// Command enricher fetches audio features and genres from Soundcharts and
// submits them to the API over gRPC.
package main

import (
	// Embedded IANA tz database: zone and DST rules come from the Go
	// toolchain, not from the base image.
	_ "time/tzdata"

	"context"
	"log/slog"

	"golang.org/x/sync/errgroup"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/enricher"
	"github.com/baoopn/spotify-tracker/internal/grpcapi"
	"github.com/baoopn/spotify-tracker/internal/heartbeat"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/app"
	"github.com/baoopn/spotify-tracker/internal/platform/ops"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/soundcharts"
)

func main() { app.Main(run) }

func run(ctx context.Context) error {
	cfg, err := config.Load[config.Enricher]()
	if err != nil {
		return err
	}
	log := logging.New("enricher", cfg.LogLevel, cfg.LogFormat)
	log.Info("starting", slog.String("version", app.Version))

	rdb, err := redisx.Connect(ctx, cfg.Redis)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()

	conn, err := grpcapi.Dial(cfg.TrackerGRPCAddr, cfg.InternalToken)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	sc := soundcharts.New(cfg.SoundchartsBaseURL, cfg.SoundchartsAppID, cfg.SoundchartsToken,
		cfg.SoundchartsRPS, cfg.SoundchartsBurst, nil)
	e := enricher.New(sc, trackerv1.NewEnrichmentServiceClient(conn), rdb, cfg.EnrichInterval(),
		cfg.BatchSize, cfg.Concurrency, heartbeat.NewReporter(rdb, "enricher"), log)
	log.Info("enricher config", slog.Float64("soundcharts_rps", cfg.SoundchartsRPS),
		slog.Int("concurrency", cfg.Concurrency))

	opsSrv := ops.NewServer(cfg.OpsAddr, log)
	opsSrv.AddCheck("sweep", e.Ready)
	opsSrv.AddCheck("redis", func(ctx context.Context) error { return rdb.Ping(ctx).Err() })

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return opsSrv.Run(gctx) })
	g.Go(func() error { return e.RunSweeps(gctx) })
	g.Go(func() error { return e.RunStream(gctx) })
	return g.Wait()
}
