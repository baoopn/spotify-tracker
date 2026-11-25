package store_test

// Integration tests against a real PostgreSQL for manual lyrics (see
// translations_pg_test.go for STORE_TEST_DATABASE_URL).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

var manual = domain.Lyrics{Status: domain.LyricsFound, Plain: "tự\nviết", Synced: "[00:01.00] tự\n[00:02.00] viết"}

func manualRow(t *testing.T, pool *pgxpool.Pool, id string) (source, status, plain string, by *string, replace bool) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `SELECT source, status, COALESCE(plain_lyrics, ''), manual_by, replace_manual
		FROM lyrics WHERE track_id = $1`, id).Scan(&source, &status, &plain, &by, &replace)
	if err != nil {
		t.Fatalf("lyrics row %s: %v", id, err)
	}
	return source, status, plain, by, replace
}

func trRequested(t *testing.T, pool *pgxpool.Pool, id string) bool {
	t.Helper()
	var req bool
	if err := pool.QueryRow(context.Background(), `SELECT requested_at IS NOT NULL FROM lyrics_translations WHERE track_id = $1`,
		id).Scan(&req); err != nil {
		t.Fatal(err)
	}
	return req
}

// Set manual lyrics: a new row, changed versus unchanged lyrics, and the
// translation re-queue (only for an existing translation, and only when the
// lyrics changed).
func TestPGManualLyricsSet(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	bare(t, pool, "new", 0)
	seed(t, pool, "tr", time.Hour) // found LRCLIB lyrics with a done translation
	exec(t, pool, `INSERT INTO lyrics_translations (track_id, status, kind, source_hash, model)
		SELECT track_id, 'done', 'synced', md5(COALESCE(synced_lyrics, '') || E'\x1f' || COALESCE(plain_lyrics, '')), 'm'
		FROM lyrics WHERE track_id = 'tr'`)

	if _, _, err := st.SetManualLyrics(ctx, "nope", manual, "me"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown track = %v", err)
	}
	stored, retr, err := st.SetManualLyrics(ctx, "new", manual, "me@x")
	if err != nil || !stored || retr {
		t.Fatalf("new = %v %v %v", stored, retr, err)
	}
	if src, s, p, by, _ := manualRow(t, pool, "new"); src != "manual" || s != domain.LyricsFound || p != manual.Plain ||
		by == nil || *by != "me@x" {
		t.Errorf("new row = %s %s %q %v", src, s, p, by)
	}
	// No translation row is created (no backfill).
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM lyrics_translations WHERE track_id = 'new'`).Scan(&n)
	if n != 0 {
		t.Error("a translation row was created")
	}
	// The same lyrics again: nothing changes.
	if stored, retr, err := st.SetManualLyrics(ctx, "new", manual, "other"); err != nil || stored || retr {
		t.Errorf("unchanged = %v %v %v", stored, retr, err)
	}
	if _, _, _, by, _ := manualRow(t, pool, "new"); *by != "me@x" {
		t.Errorf("unchanged save touched the row: by %s", *by)
	}

	// The same text as LRCLIB had: it becomes manual, the translation stays.
	var synced, plain string
	_ = pool.QueryRow(ctx, `SELECT synced_lyrics, plain_lyrics FROM lyrics WHERE track_id = 'tr'`).Scan(&synced, &plain)
	same := domain.Lyrics{Status: domain.LyricsFound, Synced: synced, Plain: plain}
	if stored, retr, err := st.SetManualLyrics(ctx, "tr", same, "me"); err != nil || !stored || retr {
		t.Errorf("same text = %v %v %v", stored, retr, err)
	}
	if trRequested(t, pool, "tr") {
		t.Error("unchanged lyrics re-queued the translation")
	}
	// Changed lyrics: the translation goes first in line.
	if stored, retr, err := st.SetManualLyrics(ctx, "tr", manual, "me"); err != nil || !stored || !retr {
		t.Errorf("changed = %v %v %v", stored, retr, err)
	}
	if !trRequested(t, pool, "tr") {
		t.Error("changed lyrics did not re-queue the translation")
	}
	// Instrumental: no text, not translated.
	exec(t, pool, `UPDATE lyrics_translations SET requested_at = NULL`)
	if stored, retr, err := st.SetManualLyrics(ctx, "tr", domain.Lyrics{Status: domain.LyricsInstrumental, Plain: "x"}, "me"); err != nil ||
		!stored || retr {
		t.Errorf("instrumental = %v %v %v", stored, retr, err)
	}
	if _, s, p, _, _ := manualRow(t, pool, "tr"); s != domain.LyricsInstrumental || p != "" {
		t.Errorf("instrumental row = %s %q", s, p)
	}
	l, err := st.TrackLyrics(ctx, "tr", true, store.TranslationCaps{})
	if err != nil || l.Source != "manual" || l.ManualAt == nil || l.ManualBy != "me" || l.Waiting || l.Queued {
		t.Errorf("TrackLyrics = %+v, %v", l, err)
	}
	nl, err := st.NowLyrics(ctx, "tr", store.TranslationCaps{})
	if err != nil || nl.Status != domain.LyricsInstrumental || nl.Source != "manual" {
		t.Errorf("NowLyrics = %+v, %v", nl, err)
	}
}

// Automatic lookups (backfill, retries, priority wakes) never claim manual
// lyrics, which never count as waiting.
func TestPGManualLyricsNotClaimed(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	bare(t, pool, "m", time.Minute)
	bare(t, pool, "other", time.Hour)
	if _, _, err := st.SetManualLyrics(ctx, "m", manual, "me"); err != nil {
		t.Fatal(err)
	}
	// Even a due retry or a stale re-queue mark does not make it claimable.
	exec(t, pool, `UPDATE lyrics SET next_attempt_at = now() - interval '1 minute' WHERE track_id = 'm'`)
	got, err := st.ClaimLyrics(ctx, "w1", []string{"m"}, 10*time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"other"}; !eq(ids(got), want) {
		t.Errorf("claim = %v, want %v", ids(got), want)
	}
	l, _ := st.TrackLyrics(ctx, "m", false, store.TranslationCaps{})
	if l.Waiting || l.Queued || l.LookingUp {
		t.Errorf("manual lyrics waiting: %+v", l)
	}
	q, err := st.LyricsQueueSummary(ctx)
	if err != nil || q.Manual != 1 || q.Found != 1 {
		t.Errorf("summary = %+v, %v", q, err)
	}
}

// A re-queue of manual lyrics is refused unless replace_manual; with it the
// next LRCLIB result replaces them, and a lookup that finds nothing keeps them.
func TestPGManualLyricsRequeue(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	bare(t, pool, "m", time.Minute)
	if _, _, err := st.SetManualLyrics(ctx, "m", manual, "me"); err != nil {
		t.Fatal(err)
	}
	if ok, err := st.RequeueLyrics(ctx, "m", false); ok || !errors.Is(err, store.ErrManualLyrics) {
		t.Fatalf("requeue = %v, %v; want ErrManualLyrics", ok, err)
	}
	if _, claimed, req := lyricsRow(t, pool, "m"); claimed != nil || req {
		t.Error("a refused re-queue changed the row")
	}
	if ok, err := st.RequeueLyrics(ctx, "nope", false); ok || err != nil {
		t.Errorf("unknown = %v, %v", ok, err)
	}

	// replace_manual, and the lookup finds nothing: the manual lyrics stay
	// and the request ends.
	if ok, err := st.RequeueLyrics(ctx, "m", true); !ok || err != nil {
		t.Fatalf("replace = %v, %v", ok, err)
	}
	if _, _, _, _, rep := manualRow(t, pool, "m"); !rep {
		t.Error("replace_manual not set")
	}
	l, _ := st.TrackLyrics(ctx, "m", false, store.TranslationCaps{})
	if !l.Queued || !l.ReplaceManual {
		t.Errorf("replace request not queued: %+v", l)
	}
	got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 5)
	if !eq(ids(got), []string{"m"}) {
		t.Fatalf("claim = %v", ids(got))
	}
	if err := st.DeferLyrics(ctx, "w1", "m", domain.LyricsNotFound, time.Now().Add(time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	if src, s, p, _, rep := manualRow(t, pool, "m"); src != "manual" || s != domain.LyricsFound || p != manual.Plain || rep {
		t.Errorf("after not found = %s %s %q replace=%v", src, s, p, rep)
	}
	if got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 5); len(got) != 0 {
		t.Errorf("claimed again: %v", ids(got))
	}

	// replace_manual, and LRCLIB finds lyrics: they replace the manual ones.
	if ok, err := st.RequeueLyrics(ctx, "m", true); !ok || err != nil {
		t.Fatal(ok, err)
	}
	got, _ = st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 5)
	if !eq(ids(got), []string{"m"}) {
		t.Fatalf("claim = %v", ids(got))
	}
	if ok, _, err := st.SetLyrics(ctx, "w1", "m", found); !ok || err != nil {
		t.Fatalf("set = %v, %v", ok, err)
	}
	if src, _, p, by, rep := manualRow(t, pool, "m"); src != "lrclib" || p != found.Plain || by != nil || rep {
		t.Errorf("after replace = %s %q by %v replace=%v", src, p, by, rep)
	}
}

// A lookup that claimed the track before the manual lyrics were saved
// cannot overwrite them; its lease is given back and is not broken early.
func TestPGManualLyricsLateLookup(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	bare(t, pool, "a", time.Minute)
	bare(t, pool, "b", time.Minute)
	got, err := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("claim = %v, %v", ids(got), err)
	}
	if ok, err := st.RequeueLyrics(ctx, "a", false); !ok || err != nil { // a re-queue during the claim
		t.Fatal(ok, err)
	}
	for _, id := range []string{"a", "b"} {
		if _, _, err := st.SetManualLyrics(ctx, id, manual, "me"); err != nil {
			t.Fatal(err)
		}
		// The live lease is kept; the re-queue mark is cleared.
		if _, by, req := lyricsRow(t, pool, id); by == nil || *by != "w1" || req {
			t.Errorf("%s: claimed by %v, requested %v", id, by, req)
		}
	}
	if ok, _, err := st.SetLyrics(ctx, "w1", "a", found); ok || err != nil {
		t.Errorf("late set = %v, %v; want not stored", ok, err)
	}
	if err := st.DeferLyrics(ctx, "w1", "b", domain.LyricsError, time.Now().Add(time.Hour), "boom"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		src, s, p, _, _ := manualRow(t, pool, id)
		_, by, req := lyricsRow(t, pool, id)
		if src != "manual" || s != domain.LyricsFound || p != manual.Plain || by != nil || req {
			t.Errorf("%s after the late lookup = %s %s %q claimed %v requested %v", id, src, s, p, by, req)
		}
		l, _ := st.TrackLyrics(ctx, id, false, store.TranslationCaps{})
		if l.LastError != "" || l.NextAttemptAt != nil || l.Waiting {
			t.Errorf("%s: %+v", id, l)
		}
	}
	// Released: ReleaseLyrics on a manual row does not re-queue it either.
	if got, _ := st.ClaimLyrics(ctx, "w2", nil, 10*time.Minute, 5); len(got) != 0 {
		t.Errorf("claimed after the late lookup: %v", ids(got))
	}
}

// Clearing manual lyrics re-queues an LRCLIB lookup at the front; a track
// that cannot be looked up loses its row.
func TestPGManualLyricsClear(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	bare(t, pool, "m", time.Hour)
	bare(t, pool, "x", time.Minute)
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms) VALUES ('noartist', 'x', 1)`)
	if ok, err := st.ClearManualLyrics(ctx, "m"); ok || err != nil {
		t.Errorf("no manual lyrics = %v, %v", ok, err)
	}
	for _, id := range []string{"m", "noartist"} {
		if _, _, err := st.SetManualLyrics(ctx, id, manual, "me"); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := st.ClearManualLyrics(ctx, "m"); !ok || err != nil {
		t.Fatalf("clear = %v, %v", ok, err)
	}
	if src, s, p, by, _ := manualRow(t, pool, "m"); src != "lrclib" || s != domain.LyricsPending || p != "" || by != nil {
		t.Errorf("cleared = %s %s %q %v", src, s, p, by)
	}
	got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 1)
	if !eq(ids(got), []string{"m"}) {
		t.Errorf("claim = %v; the cleared track goes first", ids(got))
	}
	if ok, err := st.ClearManualLyrics(ctx, "noartist"); !ok || err != nil {
		t.Fatalf("clear noartist = %v, %v", ok, err)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM lyrics WHERE track_id = 'noartist'`).Scan(&n)
	if n != 0 {
		t.Error("the row of a track that cannot be looked up was kept")
	}
	if d, err := st.TrackDurationMS(ctx, "m"); d != 200000 || err != nil {
		t.Errorf("duration = %d, %v", d, err)
	}
	if _, err := st.TrackDurationMS(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown duration = %v", err)
	}
}
