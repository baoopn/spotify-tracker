package store

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/migrations"
)

// splitColumns splits a SELECT list on top-level commas.
func splitColumns(s string) []string {
	var out []string
	depth, last := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[last:i]))
				last = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[last:]))
}

// The SELECT list and the scan targets must stay in step, and every column
// they read must exist in the migrations.
func TestTopPlaylistColumnsMatchScanAndSchema(t *testing.T) {
	cols := splitColumns(topPlaylistColumns)
	var rec domain.TopPlaylistRecord
	if n := len(topPlaylistTargets(&rec)); n != len(cols) {
		t.Fatalf("%d columns but %d scan targets: %v", len(cols), n, cols)
	}
	for i, want := range map[int]string{15: "live", 16: "refreshed_at"} {
		if cols[i] != want {
			t.Errorf("column %d = %q, want %q", i, cols[i], want)
		}
	}
	if targets := topPlaylistTargets(&rec); targets[15] != any(&rec.Live) || targets[16] != any(&rec.RefreshedAt) {
		t.Error("live / refreshed_at scan into the wrong fields")
	}

	var schema strings.Builder
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := fs.ReadFile(migrations.FS, f)
		if err != nil {
			t.Fatal(err)
		}
		up, _, _ := strings.Cut(string(b), "-- +goose Down")
		schema.WriteString(up)
	}
	ident := regexp.MustCompile(`^(?:COALESCE\()?([a-z_]+)`)
	for _, c := range cols {
		name := ident.FindStringSubmatch(c)[1]
		if !regexp.MustCompile(`(?m)(^\s+|ADD COLUMN )` + name + `\s`).MatchString(schema.String()) {
			t.Errorf("column %q is not defined by any migration", name)
		}
	}
}

func TestTopPlaylistsLiveMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00008_top_playlists_live.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	if !ok || !strings.Contains(up, "ADD COLUMN live boolean NOT NULL DEFAULT false") ||
		!strings.Contains(up, "ADD COLUMN refreshed_at timestamptz") ||
		!strings.Contains(down, "DROP COLUMN IF EXISTS refreshed_at") || !strings.Contains(down, "DROP COLUMN IF EXISTS live") {
		t.Errorf("unexpected migration:\n%s", b)
	}
}

func TestTopPlaylistsPruneMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00009_top_playlists_prune.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	for _, want := range []string{"DROP CONSTRAINT IF EXISTS top_playlists_status_check", "'pruned'",
		"ADD COLUMN pruned_at timestamptz", "ADD COLUMN spotify_removed boolean NOT NULL DEFAULT false",
		"ADD COLUMN verified_at timestamptz", "ADD COLUMN restored_at timestamptz"} {
		if !ok || !strings.Contains(up, want) {
			t.Errorf("up lacks %q", want)
		}
	}
	// Down never republishes a pruned playlist and runs before the old CHECK returns.
	set := strings.Index(down, "SET status = 'empty' WHERE status = 'pruned'")
	check := strings.Index(down, "ADD CONSTRAINT top_playlists_status_check")
	if set < 0 || check < set || strings.Contains(down, "'pruned')") || !strings.Contains(down, "DROP COLUMN IF EXISTS pruned_at") {
		t.Errorf("unexpected down:\n%s", down)
	}
	if !strings.Contains(upsertTopPlaylistQuery, "WHERE top_playlists.status <> 'pruned'") {
		t.Error("upsert may change pruned rows")
	}
}
