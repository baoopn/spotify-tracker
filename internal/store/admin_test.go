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
			_, _, err := q.AdminTracks(ctx, "ngay roi", "", "", "", 50, 0)
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
				want := "search_fold(" + col + ") LIKE search_fold(" + tc.param + ")"
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
		"IMMUTABLE STRICT PARALLEL SAFE", "normalize($1, NFD)", `'[\u0300-\u036f]'`, "'đĐðÐ', 'dDdD'", "lower(",
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
