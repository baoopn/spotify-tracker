package store

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/migrations"
)

func wantSQL(t *testing.T, sql string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(sql, p) {
			t.Errorf("SQL missing %q:\n%s", p, sql)
		}
	}
}

// strArg returns a *string argument (nil for SQL NULL).
func strArg(v any) *string {
	s, _ := v.(*string)
	return s
}

func TestTracksNeedingLyricsSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).TracksNeedingLyrics(context.Background(), 10, "now"); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "COALESCE(l.status, ''), COALESCE(l.attempts, 0)", "LEFT JOIN lyrics l ON l.track_id = t.id",
		"l.track_id IS NULL", "l.status IN ('not_found', 'error') AND l.next_attempt_at <= now()",
		"ORDER BY (t.id = $2) DESC, (l.track_id IS NULL) DESC, p.last_played DESC NULLS LAST", "LIMIT $1")
	if len(db.args) != 2 || db.args[0] != 10 || db.args[1] != "now" {
		t.Errorf("args = %v", db.args)
	}
}

func TestSetLyricsSQL(t *testing.T) {
	db := &captureDB{}
	err := (&Queries{db: db}).SetLyrics(context.Background(), "t1", domain.Lyrics{Status: domain.LyricsInstrumental})
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "INSERT INTO lyrics", "ON CONFLICT (track_id) DO UPDATE",
		"attempts = 0, last_error = NULL", "next_attempt_at = NULL")
	if len(db.args) != 5 || db.args[0] != "t1" || db.args[1] != domain.LyricsInstrumental {
		t.Fatalf("args = %v", db.args)
	}
	if id, ok := db.args[2].(*int64); !ok || id != nil {
		t.Errorf("lrclib_id = %v, want nil", db.args[2])
	}
	if strArg(db.args[3]) != nil || strArg(db.args[4]) != nil {
		t.Errorf("empty texts = %v, %v; want NULL", db.args[3], db.args[4])
	}

	db = &captureDB{}
	_ = (&Queries{db: db}).SetLyrics(context.Background(), "t1",
		domain.Lyrics{Status: domain.LyricsFound, LRCLibID: 42, Plain: "p", Synced: "s"})
	if id, _ := db.args[2].(*int64); id == nil || *id != 42 || deref(strArg(db.args[3])) != "p" || deref(strArg(db.args[4])) != "s" {
		t.Errorf("args = %v", db.args)
	}

	db = &captureDB{}
	err = (&Queries{db: db}).SetLyrics(context.Background(), "t1", domain.Lyrics{Status: domain.LyricsNotFound})
	if err == nil || errors.Is(err, errCaptured) || db.sql != "" {
		t.Errorf("not_found = %v (sql %q); want a validation error before the DB", err, db.sql)
	}
}

func TestDeferLyricsSQL(t *testing.T) {
	db := &captureDB{}
	next := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	err := (&Queries{db: db}).DeferLyrics(context.Background(), "t1", domain.LyricsError, next, "boom")
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "attempts = CASE WHEN lyrics.status = EXCLUDED.status THEN lyrics.attempts + 1 ELSE 1 END",
		"WHERE lyrics.status IN ('not_found', 'error')")
	if len(db.args) != 4 || db.args[2] != next || deref(strArg(db.args[3])) != "boom" {
		t.Errorf("args = %v", db.args)
	}
	db = &captureDB{}
	_ = (&Queries{db: db}).DeferLyrics(context.Background(), "t1", domain.LyricsNotFound, next, "")
	if strArg(db.args[3]) != nil {
		t.Errorf("empty error = %v, want NULL", db.args[3])
	}
	db = &captureDB{}
	if err := (&Queries{db: db}).DeferLyrics(context.Background(), "t1", domain.LyricsFound, next, ""); err == nil || db.sql != "" {
		t.Errorf("found = %v; want a validation error before the DB", err)
	}
}

func TestTrackLyricsSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).TrackLyrics(context.Background(), "t1", true); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "FROM tracks t LEFT JOIN lyrics l ON l.track_id = t.id", "CASE WHEN $2 THEN l.synced_lyrics END")
	if len(db.args) != 2 || db.args[0] != "t1" || db.args[1] != true {
		t.Errorf("args = %v", db.args)
	}
}

func TestSearchLyricsSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).SearchLyrics(context.Background(), "anh mơ", 10); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "websearch_to_tsquery('simple', search_fold($1))", "phraseto_tsquery('simple', search_fold($1))",
		"plainto_tsquery('simple', search_fold($1))", "l.status = 'found'")
	m := db.sql[strings.Index(db.sql, "m AS ("):strings.Index(db.sql, "SELECT m.phrase")]
	if !strings.Contains(m, "LIMIT $2") {
		t.Errorf("LIMIT $2 must be inside the m CTE:\n%s", m)
	}
	if len(db.args) != 2 || db.args[0] != "anh mơ" || db.args[1] != 10 {
		t.Errorf("args = %v", db.args)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("ngày", 10); got != "ngày" {
		t.Errorf("short = %q", got)
	}
	got := truncateRunes(strings.Repeat("ơ", 300), maxSnippet)
	if n := len([]rune(got)); n != maxSnippet || !strings.HasSuffix(got, "…") {
		t.Errorf("long = %d runes", n)
	}
}

func TestLyricsMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00014_lyrics.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	if !ok {
		t.Fatal("no Down section")
	}
	wantSQL(t, up, "CREATE TABLE lyrics", "REFERENCES tracks (id) ON DELETE CASCADE",
		"to_tsvector('simple', search_fold(", "USING gin (search_tsv)")
	if strings.Contains(strings.ToUpper(up), "CREATE EXTENSION") {
		t.Error("migration must not need an extension")
	}
	wantSQL(t, down, "DROP TABLE IF EXISTS lyrics")
}

// SearchLyrics bounds its query time (it can be called through MCP).
func TestSearchLyricsTimeout(t *testing.T) {
	db := &deadlineDB{}
	_, _ = (&Queries{db: db}).SearchLyrics(context.Background(), "anh", 10)
	if !db.ok || db.left <= 0 || db.left > searchTimeout {
		t.Errorf("deadline set = %v, left %s", db.ok, db.left)
	}
}

// deadlineDB records the context deadline of the first query.
type deadlineDB struct {
	captureDB
	ok   bool
	left time.Duration
}

func (d *deadlineDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if dl, ok := ctx.Deadline(); ok && !d.ok {
		d.ok, d.left = true, time.Until(dl)
	}
	return d.captureDB.Query(ctx, sql, args...)
}

// The public now-playing lyrics read one track's found or instrumental lyrics.
func TestNowLyricsSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).NowLyrics(context.Background(), "t1"); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "FROM tracks t", "LEFT JOIN lyrics l ON l.track_id = t.id",
		"LEFT JOIN lyrics_translations tr ON tr.track_id = l.track_id", "t.id = $1",
		"tr.source_hash = "+srcHashSQL)
	if len(db.args) != 1 || db.args[0] != "t1" {
		t.Errorf("args = %v", db.args)
	}
}
