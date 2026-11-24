package store_test

// Integration test against a real PostgreSQL, for the translation queue SQL.
// It runs only when STORE_TEST_DATABASE_URL points at a throwaway database
// (every table is migrated and written to), e.g.
//
//	STORE_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/scratch?sslmode=disable go test ./internal/store -run PG

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/platform/postgres"
	"github.com/baoopn/spotify-tracker/internal/store"
)

func pgStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("STORE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("STORE_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `TRUNCATE translation_usage, lyrics_translations, lyrics, listening_history,
		track_artists, tracks, artists CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
	return store.New(pool), pool
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", strings.Fields(sql)[0], err)
	}
}

// seed adds a track with an artist, found synced lyrics and a play ago.
func seed(t *testing.T, pool *pgxpool.Pool, id string, ago time.Duration) {
	exec(t, pool, `INSERT INTO artists (id, name) VALUES ('ar', 'Artist') ON CONFLICT DO NOTHING`)
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms) VALUES ($1, 'Song '||$1, 200000)`, id)
	exec(t, pool, `INSERT INTO track_artists (track_id, artist_id) VALUES ($1, 'ar')`, id)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, synced_lyrics, plain_lyrics)
		VALUES ($1, 'found', '[00:01.00] một
[00:02.00]
[00:03.00] ba', 'một
ba')`, id)
	exec(t, pool, `INSERT INTO listening_history (track_id, played_at) VALUES ($1, now() - $2::interval)`, id, ago)
}

func TestPGTranslationQueue(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	seed(t, pool, "old", 2*time.Hour)
	seed(t, pool, "recent", 5*time.Minute)
	seed(t, pool, "now", time.Minute)
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms) VALUES ('nolyrics', 'x', 1)`)

	// Explicit enqueue only: no backfill of the old song.
	if ok, err := st.EnqueueTranslation(ctx, "now"); err != nil || !ok {
		t.Fatalf("enqueue now = %v, %v", ok, err)
	}
	if ok, err := st.EnqueueTranslation(ctx, "now"); err != nil || !ok {
		t.Fatalf("enqueue again = %v, %v (the row exists)", ok, err)
	}
	if ok, err := st.EnqueueTranslation(ctx, "nolyrics"); err != nil || ok {
		t.Fatalf("enqueue without lyrics = %v, %v", ok, err)
	}
	if n, err := st.EnqueueRecentTranslations(ctx, 30*time.Minute); err != nil || n != 1 {
		t.Fatalf("recent = %d, %v (want only 'recent')", n, err)
	}

	// Claim: priority first, then by last play; a live lease is skipped.
	got, err := st.ClaimTranslations(ctx, "w1", "now", 10*time.Minute, 5)
	if err != nil || len(got) != 2 || got[0].TrackID != "now" || got[1].TrackID != "recent" {
		t.Fatalf("claim = %+v, %v", got, err)
	}
	if got[0].Name != "Song now" || got[0].Artist != "Artist" || !strings.Contains(got[0].Synced, "một") || got[0].SourceHash == "" {
		t.Errorf("candidate = %+v", got[0])
	}
	if again, _ := st.ClaimTranslations(ctx, "w2", "", 10*time.Minute, 5); len(again) != 0 {
		t.Fatalf("live leases claimed again: %+v", again)
	}
	// An expired lease is claimed again.
	exec(t, pool, `UPDATE lyrics_translations SET claimed_until = now() - interval '1 second' WHERE track_id = 'recent'`)
	if again, _ := st.ClaimTranslations(ctx, "w2", "", 10*time.Minute, 5); len(again) != 1 || again[0].TrackID != "recent" {
		t.Fatalf("expired lease = %+v", again)
	}

	// Results are written only by the claim holder.
	tr := domain.Translation{Kind: domain.TranslationSynced, SourceLanguage: "vi", EN: []string{"one", "", "three"}, Model: "m"}
	if ok, err := st.SetTranslation(ctx, "w2", "now", got[0].SourceHash, tr); err != nil || ok {
		t.Fatalf("foreign set = %v, %v", ok, err)
	}
	// A re-queue during the lease keeps the lease and the song stays queued.
	if ok, err := st.RequeueTranslation(ctx, "now"); err != nil || !ok {
		t.Fatalf("requeue = %v, %v", ok, err)
	}
	if ok, err := st.SetTranslation(ctx, "w1", "now", got[0].SourceHash, tr); err != nil || !ok {
		t.Fatalf("set = %v, %v", ok, err)
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM lyrics_translations WHERE track_id = 'now'`).Scan(&status)
	if status != "pending" {
		t.Errorf("re-queued during the lease: status %q, want pending", status)
	}
	// Claimed again (requested first), stored done this time.
	c2, _ := st.ClaimTranslations(ctx, "w1", "", 10*time.Minute, 1)
	if len(c2) != 1 || c2[0].TrackID != "now" {
		t.Fatalf("re-queued claim = %+v", c2)
	}
	if ok, err := st.SetTranslation(ctx, "w1", "now", c2[0].SourceHash, tr); err != nil || !ok {
		t.Fatalf("set again = %v, %v", ok, err)
	}

	caps := store.TranslationCaps{Daily: 2, Monthly: 10}
	nl, err := st.NowLyrics(ctx, "now", caps)
	if err != nil || nl.Status != domain.LyricsFound || nl.Translation == nil || nl.Translation.Status != domain.TranslationDone ||
		strings.Join(nl.Translation.EN, "|") != "one||three" || nl.Translation.VI != nil {
		t.Fatalf("now lyrics = %+v %+v, %v", nl, nl.Translation, err)
	}
	if nl, _ := st.NowLyrics(ctx, "nolyrics", caps); nl.Status != domain.LyricsPending {
		t.Errorf("no lyrics row = %+v, want pending", nl)
	}
	tl, err := st.TrackLyrics(ctx, "now", true, caps)
	if err != nil || tl.Translation == nil || tl.Translation.Status != "done" || tl.Translation.Waiting ||
		tl.Translation.EN == nil || len(strings.Split(*tl.Translation.EN, "\n")) != 3 {
		t.Fatalf("track lyrics = %+v, %v", tl.Translation, err)
	}

	// A far retry is not pending; a retry due within minutes is.
	far := time.Now().Add(time.Hour)
	if err := st.DeferTranslation(ctx, "w2", "recent", got[1].SourceHash, domain.TranslationError, &far, "boom"); err != nil {
		t.Fatal(err)
	}
	if nl, _ := st.NowLyrics(ctx, "recent", caps); nl.Translation == nil || nl.Translation.Status != "" {
		t.Errorf("far retry = %+v, want not pending", nl.Translation)
	}
	exec(t, pool, `UPDATE lyrics_translations SET next_attempt_at = now() + interval '30 seconds' WHERE track_id = 'recent'`)
	if nl, _ := st.NowLyrics(ctx, "recent", caps); nl.Translation.Status != domain.TranslationPending {
		t.Errorf("soon retry = %+v, want pending", nl.Translation)
	}

	// Caps: the reserve is atomic and stops at the daily cap; a reached cap
	// is not pending.
	day := time.Now().UTC()
	for i, want := range []bool{true, true, false} {
		if ok, err := st.ReserveTranslationCall(ctx, day, 2, 10); err != nil || ok != want {
			t.Fatalf("reserve %d = %v, %v; want %v", i, ok, err, want)
		}
	}
	if nl, _ := st.NowLyrics(ctx, "recent", caps); nl.Translation.Status != "" {
		t.Errorf("capped = %+v, want not pending", nl.Translation)
	}
	if err := st.AddTranslationTokens(ctx, day, 100, 40); err != nil {
		t.Fatal(err)
	}

	sum, err := st.TranslationQueueSummary(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Done != 1 || sum.Error != 1 || sum.CallsToday != 2 || sum.CallsMonth != 2 ||
		sum.TokensMonth.Input != 100 || sum.NextRetryAt == nil {
		t.Errorf("summary = %+v", sum)
	}
}
