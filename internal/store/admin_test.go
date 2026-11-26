package store

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/spotify-tracker/migrations"
)

var errCaptured = errors.New("captured")

// captureDB records the first statement and fails it, so a query method
// returns right after building its SQL.
type captureDB struct {
	sql  string
	args []any
}

func (c *captureDB) record(sql string, args []any) {
	if c.sql == "" {
		c.sql, c.args = sql, args
	}
}

func (c *captureDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.record(sql, args)
	return pgconn.CommandTag{}, errCaptured
}

func (c *captureDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.record(sql, args)
	return nil, errCaptured
}

func (c *captureDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	c.record(sql, args)
	return errRow{}
}

func (c *captureDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

type errRow struct{}

func (errRow) Scan(...any) error { return errCaptured }

// Name searches fold accents and case on both sides; ISRC stays a plain ILIKE.
func TestAdminSearchFoldsNames(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		run   func(q *Queries) error
		param string
		fold  []string
		other []string
	}{
		{"history", func(q *Queries) error {
			_, err := q.AdminHistory(ctx, 50, time.Time{}, "Ngày rời")
			return err
		}, "$3", []string{"t.name", "al.name", "a.name"}, nil},
		{"tracks", func(q *Queries) error {
			_, _, err := q.AdminTracks(ctx, "ngay roi", "", "", "", "", 50, 0)
			return err
		}, "$1", []string{"t.name", "al.name", "a.name"}, []string{"t.isrc ILIKE $1"}},
		{"artists", func(q *Queries) error {
			_, _, err := q.AdminArtists(ctx, "Sơn Tùng", "", "", 50, 0)
			return err
		}, "$1", []string{"a.name"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := &captureDB{}
			if err := tc.run(&Queries{db: db}); !errors.Is(err, errCaptured) {
				t.Fatalf("err = %v", err)
			}
			for _, col := range tc.fold {
				want := "search_fold(" + col + ") LIKE (SELECT search_fold(" + tc.param + "))"
				if !strings.Contains(db.sql, want) {
					t.Errorf("sql lacks %q:\n%s", want, db.sql)
				}
			}
			for _, want := range tc.other {
				if !strings.Contains(db.sql, want) {
					t.Errorf("sql lacks %q:\n%s", want, db.sql)
				}
			}
			if m := regexp.MustCompile(`\w+\.name ILIKE`).FindString(db.sql); m != "" {
				t.Errorf("name column still uses ILIKE (%q):\n%s", m, db.sql)
			}
		})
	}
}

// Without a search term no fold filter is added.
func TestAdminSearchEmptyHasNoFilter(t *testing.T) {
	db := &captureDB{}
	_, _, _ = (&Queries{db: db}).AdminArtists(context.Background(), "  ", "", "", 50, 0)
	if strings.Contains(db.sql, "search_fold") || len(db.args) != 0 {
		t.Errorf("unexpected filter: %s %v", db.sql, db.args)
	}
}

func TestLikePatternEscapes(t *testing.T) {
	if got, want := likePattern(` 50%_off\ `), `%50\%\_off\\%`; got != want {
		t.Errorf("likePattern = %q, want %q", got, want)
	}
}

func TestSearchFoldMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00010_search_fold.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	for _, want := range []string{"CREATE OR REPLACE FUNCTION search_fold(text) RETURNS text",
		"IMMUTABLE STRICT PARALLEL SAFE", "normalize(lower($1), NFD)", `'[\u0300-\u036f]'`, "'đðłø', 'ddlo'",
		"-- +goose StatementBegin"} {
		if !ok || !strings.Contains(up, want) {
			t.Errorf("up lacks %q", want)
		}
	}
	if strings.Contains(up, "CREATE EXTENSION") {
		t.Error("search_fold must not need an extension")
	}
	if !strings.Contains(down, "DROP FUNCTION IF EXISTS search_fold(text)") {
		t.Errorf("unexpected down:\n%s", down)
	}
}

// The lyrics filter adds a fixed condition on the joined lyrics row and no
// parameter; an unknown value adds nothing.
func TestAdminTracksLyricsFilter(t *testing.T) {
	ctx := context.Background()
	for filter, want := range map[string]string{
		LyricsFilterMissing:      "(COALESCE(l.status, 'pending') IN ('pending', 'not_found', 'error'))",
		LyricsFilterUnsynced:     "l.status = 'found' AND COALESCE(l.plain_lyrics, '') <> '' AND COALESCE(l.synced_lyrics, '') = ''",
		LyricsFilterFound:        "(l.status = 'found')",
		LyricsFilterInstrumental: "(l.status = 'instrumental')",
	} {
		db := &captureDB{}
		_, _, _ = (&Queries{db: db}).AdminTracks(ctx, "", "enriched", filter, "", "", 50, 0)
		if !strings.Contains(db.sql, want) || !strings.Contains(db.sql, "LEFT JOIN lyrics l ON l.track_id = t.id") {
			t.Errorf("%s: sql lacks %q:\n%s", filter, want, db.sql)
		}
		if len(db.args) != 1 {
			t.Errorf("%s: args = %v, want only the status", filter, db.args)
		}
	}
	db := &captureDB{}
	_, _, _ = (&Queries{db: db}).AdminTracks(ctx, "", "", "bogus", "", "", 50, 0)
	if strings.Contains(db.sql, "l.status") || len(db.args) != 0 {
		t.Errorf("unknown filter changed the query: %s %v", db.sql, db.args)
	}
	for _, f := range []string{"", "missing", "unsynced", "found", "instrumental"} {
		if !ValidLyricsFilter(f) {
			t.Errorf("ValidLyricsFilter(%q) = false", f)
		}
	}
	for _, f := range []string{"pending", "not_found", "MISSING", "missing;"} {
		if ValidLyricsFilter(f) {
			t.Errorf("ValidLyricsFilter(%q) = true", f)
		}
	}
}

// The overview's missing-lyrics count uses the same condition as the tracks
// filter.
func TestLyricsMissingCountSharesFilter(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).LyricsMissingCount(context.Background()); !errors.Is(err, errCaptured) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(db.sql, lyricsFilters[LyricsFilterMissing]) || !strings.Contains(db.sql, "LEFT JOIN lyrics l ON l.track_id = t.id") {
		t.Errorf("sql does not use the missing filter:\n%s", db.sql)
	}
}
