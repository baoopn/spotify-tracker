package store

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/migrations"
)

// migrationText returns the up part of every migration, concatenated.
func migrationText(t *testing.T) string {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		up, _, _ := strings.Cut(string(data), "-- +goose Down")
		b.WriteString(up)
	}
	return b.String()
}

// trackColumns, scanTrack and albumScan must change in step.
func TestTrackColumnsMatchScan(t *testing.T) {
	cols := strings.Split(trackColumns, ",")
	// COALESCE(x, '') splits into two parts; join them back.
	var exprs []string
	for i := 0; i < len(cols); i++ {
		c := strings.TrimSpace(cols[i])
		if strings.HasPrefix(c, "COALESCE(") && !strings.HasSuffix(c, ")") {
			i++
			c += "," + cols[i]
		}
		exprs = append(exprs, c)
	}
	var tv domain.TrackView
	var al albumScan
	if n := len(scanTrack(&tv)) + len(al.targets()); n != len(exprs) {
		t.Fatalf("trackColumns has %d expressions, scan has %d targets", len(exprs), n)
	}
	schema := migrationText(t)
	colRe := regexp.MustCompile(`\b(t|al)\.(\w+)`)
	for _, e := range exprs {
		m := colRe.FindStringSubmatch(e)
		if m == nil {
			t.Errorf("no column in %q", e)
			continue
		}
		table := map[string]string{"t": "tracks", "al": "albums"}[m[1]]
		if !regexp.MustCompile(`(?s)(CREATE TABLE ` + table + `\b[^;]*\b` + m[2] + `\b|ALTER TABLE ` + table +
			`\b[^;]*ADD COLUMN ` + m[2] + `\b)`).MatchString(schema) {
			t.Errorf("column %s.%s not found in the migrations", table, m[2])
		}
	}
}

func TestNowPlayingSelectsTrackColumns(t *testing.T) {
	db := &captureDB{}
	_, _ = (&Queries{db: db}).NowPlaying(context.Background())
	for _, want := range []string{"t.track_number, t.disc_number,",
		"al.album_type, al.total_tracks, al.release_date_precision"} {
		if !strings.Contains(db.sql, want) {
			t.Errorf("NowPlaying lacks %q:\n%s", want, db.sql)
		}
	}
}

func TestSpotifyMetadataMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00013_spotify_metadata.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	if !ok {
		t.Fatal("no down section")
	}
	for _, want := range []string{"ADD COLUMN track_number integer", "ADD COLUMN disc_number  integer",
		"ADD COLUMN album_type             text", "ADD COLUMN total_tracks           integer",
		"ADD COLUMN release_date_precision text", "ADD COLUMN upc                    text",
		"ADD COLUMN copyrights             jsonb", "ADD COLUMN details_fetched_at     timestamptz",
		"CREATE INDEX albums_details_fetched_idx ON albums (details_fetched_at NULLS FIRST)",
		"ADD COLUMN genres            text[]", "ADD COLUMN genres_fetched_at timestamptz"} {
		if !strings.Contains(up, want) {
			t.Errorf("up lacks %q", want)
		}
	}
	for _, bad := range []string{"CHECK", "DEFAULT", "NOT NULL"} {
		if strings.Contains(up, bad) {
			t.Errorf("up must not contain %s", bad)
		}
	}
	for _, want := range []string{"DROP COLUMN IF EXISTS genres_fetched_at", "DROP COLUMN IF EXISTS genres",
		"DROP INDEX IF EXISTS albums_details_fetched_idx", "DROP COLUMN IF EXISTS details_fetched_at",
		"DROP COLUMN IF EXISTS copyrights", "DROP COLUMN IF EXISTS upc",
		"DROP COLUMN IF EXISTS release_date_precision", "DROP COLUMN IF EXISTS total_tracks",
		"DROP COLUMN IF EXISTS album_type", "DROP COLUMN IF EXISTS disc_number",
		"DROP COLUMN IF EXISTS track_number"} {
		if !strings.Contains(down, want) {
			t.Errorf("down lacks %q", want)
		}
	}
}

func TestUpsertAlbumKeepsMetadata(t *testing.T) {
	for _, tc := range []struct {
		in   domain.Album
		want []any
	}{
		{domain.Album{ID: "al1", Name: "A", AlbumType: "single", TotalTracks: ptr(int32(2)), ReleaseDatePrecision: "day"},
			[]any{ptr("single"), ptr(int32(2)), ptr("day")}},
		{domain.Album{ID: "al1", Name: "A"}, []any{(*string)(nil), (*int32)(nil), (*string)(nil)}},
	} {
		db := &captureDB{}
		_ = (&Queries{db: db}).UpsertAlbum(context.Background(), tc.in)
		for _, col := range []string{"album_type", "total_tracks", "release_date_precision"} {
			if want := col + " = COALESCE(EXCLUDED." + col + ", albums." + col + ")"; !strings.Contains(db.sql, want) {
				t.Errorf("sql lacks %q", want)
			}
		}
		if len(db.args) != 9 || !reflect.DeepEqual(db.args[6:], tc.want) {
			t.Errorf("args = %v, want ... %v", db.args, tc.want)
		}
	}
}

func TestUpsertTrackKeepsNumbers(t *testing.T) {
	db := &captureDB{}
	_ = (&Queries{db: db}).UpsertTrack(context.Background(),
		domain.Track{ID: "t1", Name: "T", TrackNumber: ptr(int32(3))}, nil)
	for _, col := range []string{"track_number", "disc_number"} {
		if want := col + " = COALESCE(EXCLUDED." + col + ", tracks." + col + ")"; !strings.Contains(db.sql, want) {
			t.Errorf("sql lacks %q", want)
		}
	}
	if len(db.args) != 10 || !reflect.DeepEqual(db.args[8:], []any{ptr(int32(3)), (*int32)(nil)}) {
		t.Errorf("args = %v", db.args)
	}
}

func TestSetArtistProfileGenres(t *testing.T) {
	cases := []struct {
		genres []string
		want   any
	}{
		{nil, nil},
		{[]string{}, []string{}},
		{[]string{"pop"}, []string{"pop"}},
	}
	for _, tc := range cases {
		db := &captureDB{}
		if err := (&Queries{db: db}).SetArtistProfile(context.Background(), "a1", "", tc.genres); !errors.Is(err, errCaptured) {
			t.Fatalf("err = %v", err)
		}
		for _, want := range []string{"image_url = COALESCE($2, image_url)", "genres = COALESCE($3, genres)",
			"image_fetched_at = now()", "genres_fetched_at = now()"} {
			if !strings.Contains(db.sql, want) {
				t.Errorf("sql lacks %q", want)
			}
		}
		if db.args[1] != (*string)(nil) {
			t.Errorf("empty image url not NULL: %v", db.args[1])
		}
		if !reflect.DeepEqual(db.args[2], tc.want) {
			t.Errorf("genres %v: arg = %#v, want %#v", tc.genres, db.args[2], tc.want)
		}
	}
}

func TestArtistsNeedingProfileSQL(t *testing.T) {
	db := &captureDB{}
	stale := time.Now()
	_, _ = (&Queries{db: db}).ArtistsNeedingProfile(context.Background(), stale, 25)
	for _, want := range []string{
		"WHERE a.image_fetched_at IS NULL OR a.genres_fetched_at IS NULL OR a.image_fetched_at < $1",
		"ORDER BY (a.image_fetched_at IS NULL) DESC, (a.genres_fetched_at IS NULL) DESC,",
		"p.last_played DESC NULLS LAST, a.image_fetched_at, a.id", "LIMIT $2"} {
		if !strings.Contains(db.sql, want) {
			t.Errorf("sql lacks %q:\n%s", want, db.sql)
		}
	}
	if !reflect.DeepEqual(db.args, []any{stale, 25}) {
		t.Errorf("args = %v", db.args)
	}
}

func TestSetAlbumDetailsNil(t *testing.T) {
	db := &captureDB{}
	_ = (&Queries{db: db}).SetAlbumDetails(context.Background(), "al1", nil)
	if strings.TrimSpace(db.sql) != "UPDATE albums SET details_fetched_at = now() WHERE id = $1" ||
		!reflect.DeepEqual(db.args, []any{"al1"}) {
		t.Errorf("got %q %v", db.sql, db.args)
	}
}

func TestSetAlbumDetails(t *testing.T) {
	db := &captureDB{}
	d := &domain.AlbumDetails{AlbumType: "album", TotalTracks: ptr(int32(12)), UPC: "006",
		Copyrights: []domain.Copyright{{Text: "© 2020 L", Type: "C"}},
		Tracks:     []domain.AlbumTrackNumber{{TrackID: "t1", TrackNumber: ptr(int32(1))}}}
	_ = (&Queries{db: db}).SetAlbumDetails(context.Background(), "al1", d)
	for _, want := range []string{"WITH a AS (", "copyrights = $6::jsonb, details_fetched_at = now()",
		"upc = COALESCE($5, upc)", "WHERE id = $1 RETURNING id)",
		"FROM a, unnest($7::text[], $8::int[], $9::int[]) AS u(id, tn, dn)",
		"WHERE t.id = u.id AND t.album_id = a.id",
		"track_number = COALESCE(u.tn, t.track_number)", "disc_number = COALESCE(u.dn, t.disc_number)",
		"(u.tn IS NOT NULL AND t.track_number IS DISTINCT FROM u.tn)",
		"(u.dn IS NOT NULL AND t.disc_number IS DISTINCT FROM u.dn)"} {
		if !strings.Contains(db.sql, want) {
			t.Errorf("sql lacks %q:\n%s", want, db.sql)
		}
	}
	want := []any{"al1", ptr("album"), ptr(int32(12)), (*string)(nil), ptr("006"),
		`[{"text":"© 2020 L","type":"C"}]`, []string{"t1"}, []*int32{ptr(int32(1))}, []*int32{nil}}
	if !reflect.DeepEqual(db.args, want) {
		t.Errorf("args = %#v", db.args)
	}

	// No copyrights and no tracks: '[]' and empty (not NULL) arrays.
	db = &captureDB{}
	_ = (&Queries{db: db}).SetAlbumDetails(context.Background(), "al1", &domain.AlbumDetails{})
	if db.args[5] != "[]" || !reflect.DeepEqual(db.args[6], []string{}) || !reflect.DeepEqual(db.args[7], []*int32{}) {
		t.Errorf("empty args = %#v", db.args)
	}
}

func TestAlbumsNeedingDetailsSQL(t *testing.T) {
	db := &captureDB{}
	stale := time.Now()
	_, _ = (&Queries{db: db}).AlbumsNeedingDetails(context.Background(), stale, 15)
	for _, want := range []string{"WHERE al.details_fetched_at IS NULL OR al.details_fetched_at < $1",
		"OR (al.copyrights IS NULL AND al.details_fetched_at < now() - interval '7 days')",
		"ORDER BY al.details_fetched_at NULLS FIRST, p.last_played DESC NULLS LAST, al.id",
		"LEFT JOIN LATERAL", "MAX(h.played_at)", "WHERE t.album_id = al.id", "LIMIT $2"} {
		if !strings.Contains(db.sql, want) {
			t.Errorf("sql lacks %q:\n%s", want, db.sql)
		}
	}
	if !reflect.DeepEqual(db.args, []any{stale, 15}) {
		t.Errorf("args = %v", db.args)
	}
}

func TestDeferAlbumDetails(t *testing.T) {
	db := &captureDB{}
	_ = (&Queries{db: db}).DeferAlbumDetails(context.Background(), "al1", 179*24*time.Hour)
	if !strings.Contains(db.sql, "SET details_fetched_at = now() - make_interval(secs => $2::bigint)") ||
		strings.Contains(db.sql, "copyrights") || !reflect.DeepEqual(db.args, []any{"al1", int64(179 * 24 * 3600)}) {
		t.Errorf("got %q %v", db.sql, db.args)
	}
}

// The public track detail never shows the album lookup time.
func TestHideAdminOnly(t *testing.T) {
	now := time.Now()
	v := domain.TrackView{Album: &domain.Album{ID: "al1", UPC: "006", DetailsFetchedAt: &now}}
	hideAdminOnly(&v)
	if v.Album.DetailsFetchedAt != nil || v.Album.UPC != "006" {
		t.Errorf("album = %+v", v.Album)
	}
	hideAdminOnly(&domain.TrackView{}) // no album: no panic
}

func ptr[T any](v T) *T { return &v }
