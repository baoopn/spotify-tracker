// Package postgres provides the pgx connection pool and schema migrations.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/migrations"
)

// Connect opens a pgx pool and verifies connectivity.
func Connect(ctx context.Context, cfg config.Postgres) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse url: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = cfg.MinConns
	pc.MaxConnLifetime = cfg.MaxConnLifetime
	pc.ConnConfig.RuntimeParams["application_name"] = "spotify-tracker"
	pc.ConnConfig.RuntimeParams["timezone"] = "UTC"

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}

	// Retry briefly so containers tolerate the database starting slightly later.
	var pingErr error
	for attempt := 1; attempt <= 10; attempt++ {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		pingErr = pool.Ping(pctx)
		cancel()
		if pingErr == nil {
			return pool, nil
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
	}
	pool.Close()
	return nil, fmt.Errorf("postgres: ping: %w", pingErr)
}

// Migrate applies all pending migrations. A Postgres advisory lock makes it
// safe to call from several replicas at once.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()
	return migrate(ctx, db, log)
}

func migrate(ctx context.Context, db *sql.DB, log *slog.Logger) error {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migrate: locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS,
		goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("migrate: provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrate: up: %w", err)
	}
	for _, r := range results {
		log.Info("migration applied", slog.String("source", r.Source.Path), slog.Duration("duration", r.Duration))
	}
	v, err := provider.GetDBVersion(ctx)
	if err == nil {
		log.Info("database schema up to date", slog.Int64("version", v))
	}
	return nil
}
