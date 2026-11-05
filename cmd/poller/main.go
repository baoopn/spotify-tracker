// Command poller polls Spotify "currently playing" and forwards snapshots to
// the API over gRPC. It holds no Spotify secrets: access tokens come from the
// API's SpotifyService.
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
	"github.com/baoopn/spotify-tracker/internal/grpcapi"
	"github.com/baoopn/spotify-tracker/internal/heartbeat"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/app"
	"github.com/baoopn/spotify-tracker/internal/platform/ops"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/poller"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

func main() { app.Main(run) }

func run(ctx context.Context) error {
	cfg, err := config.Load[config.Poller]()
	if err != nil {
		return err
	}
	log := logging.New("poller", cfg.LogLevel, cfg.LogFormat)
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

	tokens := grpcapi.NewTokenSource(trackerv1.NewSpotifyServiceClient(conn))
	player := spotify.NewPlayer(tokens, "", nil)
	p := poller.New(player, trackerv1.NewIngestServiceClient(conn), cfg.PollInterval(),
		heartbeat.NewReporter(rdb, "poller"), log)

	opsSrv := ops.NewServer(cfg.OpsAddr, log)
	opsSrv.AddCheck("poll", p.Ready)
	opsSrv.AddCheck("redis", func(ctx context.Context) error { return rdb.Ping(ctx).Err() })

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return opsSrv.Run(gctx) })
	g.Go(func() error { return p.Run(gctx) })
	return g.Wait()
}
