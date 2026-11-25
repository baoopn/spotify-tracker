package tracker_test

// Integration test against a real PostgreSQL for queue prefetch. It runs
// only when STORE_TEST_DATABASE_URL points at a throwaway database (every
// table is migrated and written to). Redis is not needed: the client points
// at a closed port, so only the database effects are checked here.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/platform/postgres"
	"github.com/baoopn/spotify-tracker/internal/store"
	"github.com/baoopn/spotify-tracker/internal/tracker"
)

const (
	pgCurrent = "1111111111111111111111"
	pgNext    = "2222222222222222222222"
	pgLater   = "3333333333333333333333"
)

// pgPool connects to STORE_TEST_DATABASE_URL in its own schema ("tracker_test"),
// so test packages running against the same database never touch each
// other's tables. It skips the test when the variable is unset.
func pgPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("STORE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("STORE_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS tracker_test")
	admin.Close()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "tracker_test"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	return pool
}

func pgService(t *testing.T) (*tracker.Service, *store.Store, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool := pgPool(t)
	if _, err := pool.Exec(ctx, `TRUNCATE listening_history, track_features, track_artists, tracks, albums,
		artists, current_playback CASCADE`); err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	st := store.New(pool)
	return tracker.NewService(st, rdb, slog.New(slog.DiscardHandler), tracker.PlayRules{}, 8), st, pool
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func upcoming(id string, prefetch bool) domain.Upcoming {
	return domain.Upcoming{
		CatalogEntry: domain.CatalogEntry{
			Track:   domain.Track{ID: id, Name: "Song " + id, DurationMS: 200_000, ISRC: "ISRC" + id[:3]},
			Album:   domain.Album{ID: "alb" + id[:3], Name: "Album", AlbumType: "album"},
			Artists: []domain.Artist{{ID: "art" + id[:3], Name: "Artist"}},
		},
		Prefetch: prefetch,
	}
}

func TestPGPrefetchUpcoming(t *testing.T) {
	svc, st, pool := pgService(t)
	ctx := context.Background()
	var hooked []string
	svc.OnUpcoming(func(ids []string) { hooked = append(hooked, ids...) })
	listenBefore := count(t, pool, `SELECT count(*) FROM listen_state WHERE track_id <> '' OR last_seen IS NOT NULL`)

	ids, err := svc.PrefetchUpcoming(ctx, pgCurrent, []domain.Upcoming{
		upcoming(pgNext, true), upcoming(pgLater, false), upcoming(pgCurrent, true),
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []string{pgNext}) || !slices.Equal(hooked, []string{pgNext}) {
		t.Fatalf("ids %v hooked %v", ids, hooked)
	}
	// Catalog only: never a play, a feature row, the listen or now playing.
	if n := count(t, pool, `SELECT count(*) FROM tracks WHERE id = $1 AND album_id = $2`, pgNext, "alb222"); n != 1 {
		t.Errorf("track rows = %d", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM artists WHERE id = 'art222'`); n != 1 {
		t.Errorf("artist rows = %d", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM tracks WHERE id = $1`, pgLater); n != 0 {
		t.Error("a listed-only entry was stored")
	}
	for _, q := range []string{
		`SELECT count(*) FROM listening_history`,
		`SELECT count(*) FROM track_features`,
		`SELECT count(*) FROM current_playback`,
	} {
		if n := count(t, pool, q); n != 0 {
			t.Errorf("%s = %d, want 0", q, n)
		}
	}
	if n := count(t, pool, `SELECT count(*) FROM listen_state WHERE track_id <> '' OR last_seen IS NOT NULL`); n != listenBefore {
		t.Errorf("listen_state changed: %d -> %d", listenBefore, n)
	}
	// The public track detail does not reveal a never-played track.
	if _, err := st.TrackDetail(ctx, pgNext); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("track detail err = %v, want not found", err)
	}

	// The real play still creates the enrichment row (the catalog-seen
	// marker was not set by prefetch).
	p := int32(1000)
	if _, err := svc.RecordPlayback(ctx, domain.Playback{
		Track: upcoming(pgNext, true).Track, Album: upcoming(pgNext, true).Album,
		Artists: upcoming(pgNext, true).Artists, IsPlaying: true, ProgressMS: &p, FetchedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM track_features WHERE track_id = $1`, pgNext); n != 1 {
		t.Errorf("track_features after the play = %d, want 1", n)
	}
	// A first play is found while it plays, before it counts (30%).
	if n := count(t, pool, `SELECT count(*) FROM listening_history`); n != 0 {
		t.Fatalf("history after 1 s = %d", n)
	}
	if _, err := st.TrackDetail(ctx, pgNext); err != nil {
		t.Errorf("track detail while it plays: %v", err)
	}
}

func TestPGPrefetchEnrich(t *testing.T) {
	svc, _, pool := pgService(t)
	svc.SetPrefetchOptions(tracker.PrefetchOptions{Enrich: true})
	if _, err := svc.PrefetchUpcoming(context.Background(), pgCurrent,
		[]domain.Upcoming{upcoming(pgNext, true)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM track_features WHERE track_id = $1`, pgNext); n != 1 {
		t.Errorf("track_features = %d, want 1 with enrichment on", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM listening_history`); n != 0 {
		t.Errorf("history rows = %d", n)
	}
}
