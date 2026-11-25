package store

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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

func TestTrackLyricsSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).TrackLyrics(context.Background(), "t1", true, TranslationCaps{Daily: 100}); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "FROM tracks t LEFT JOIN lyrics l ON l.track_id = t.id", "CASE WHEN $2 THEN l.synced_lyrics END")
	wantSQL(t, db.sql, "LEFT JOIN lyrics_translations tr ON tr.track_id = l.track_id", "AS reached) cap")
	if len(db.args) != 4 || db.args[0] != "t1" || db.args[1] != true || db.args[2] != 100 || db.args[3] != 0 {
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
	if _, err := (&Queries{db: db}).NowLyrics(context.Background(), "t1", TranslationCaps{Daily: 5, Monthly: 50}); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "FROM tracks t", "LEFT JOIN lyrics l ON l.track_id = t.id",
		"LEFT JOIN lyrics_translations tr ON tr.track_id = l.track_id", "t.id = $1",
		"tr.source_hash = "+srcHashSQL)
	if len(db.args) != 3 || db.args[0] != "t1" || db.args[1] != 5 || db.args[2] != 50 {
		t.Errorf("args = %v", db.args)
	}
}
