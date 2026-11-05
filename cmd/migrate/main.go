// Command migrate applies database migrations and exits.
package main

import (
	// Embedded IANA tz database: zone and DST rules come from the Go
	// toolchain, not from the base image.
	_ "time/tzdata"

	"context"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/app"
	"github.com/baoopn/spotify-tracker/internal/platform/postgres"
)

func main() { app.Main(run) }

func run(ctx context.Context) error {
	cfg, err := config.Load[config.Migrate]()
	if err != nil {
		return err
	}
	log := logging.New("migrate", cfg.LogLevel, cfg.LogFormat)
	pool, err := postgres.Connect(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()
	return postgres.Migrate(ctx, pool, log)
}
