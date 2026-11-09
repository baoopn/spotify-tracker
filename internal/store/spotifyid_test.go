package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const testID = "4uLU6hMCjMI75M1A2tKUQC"

func TestSpotifyRef(t *testing.T) {
	cases := []struct {
		in, kind string
		ok       bool
	}{
		{testID, "", true},
		{"  " + testID + "\n", "", true},
		{"spotify:track:" + testID, "track", true},
		{"spotify:artist:" + testID, "artist", true},
		{"spotify:album:" + testID, "album", true},
		{"https://open.spotify.com/track/" + testID, "track", true},
		{"https://open.spotify.com/track/" + testID + "?si=abc123", "track", true},
		{"https://open.spotify.com/artist/" + testID + "#x", "artist", true},
		{"https://open.spotify.com/album/" + testID + "/", "album", true},
		{"http://open.spotify.com/track/" + testID, "track", true},
		{"https://www.open.spotify.com/track/" + testID, "track", true},
		{"https://open.spotify.com/intl-de/track/" + testID + "?si=1", "track", true},
		{"https://open.spotify.com/intl-pt-br/album/" + testID, "album", true},
		{"https://open.spotify.com/embed/track/" + testID, "track", true},
		{"HTTPS://OPEN.SPOTIFY.COM/track/" + testID, "track", true},

		// Near misses and hostile input: no ID match.
		{"", "", false},
		{"Ngày rời", "", false},
		{testID[:21], "", false},
		{testID + "X", "", false},
		{"4uLU6hMCjMI75M1A2tKU-C", "", false},
		{"4uLU6hMCjMI75M1A2tKUQ'", "", false},
		{"' OR 1=1 --xxxxxxxxxxx", "", false},
		{"spotify:playlist:" + testID, "", false},
		{"spotify:track:" + testID + "x", "", false},
		{"spotify:track:" + testID + " OR 1=1", "", false},
		{"SPOTIFY:TRACK:" + testID, "", false},
		{"https://open.spotify.com/playlist/" + testID, "", false},
		{"https://open.spotify.com/track/" + testID + "x", "", false},
		{"https://evil.com/open.spotify.com/track/" + testID, "", false},
		{"https://open.spotify.com.evil.com/track/" + testID, "", false},
		{"https://open.spotify.com/track/" + testID + "?si=a b", "", false},
		{"ftp://open.spotify.com/track/" + testID, "", false},
		{"open.spotify.com/track/" + testID, "", false},
		{"https://open.spotify.com/intl-deu/track/" + testID, "", false},
		{"https://open.spotify.com/user/x/track/" + testID, "", false},
		{testID + "\n" + testID, "", false},
	}
	for _, tc := range cases {
		kind, id, ok := spotifyRef(tc.in)
		if ok != tc.ok || kind != tc.kind || (ok && id != testID) || (!ok && id != "") {
			t.Errorf("spotifyRef(%q) = %q, %q, %v; want %q, %v", tc.in, kind, id, ok, tc.kind, tc.ok)
		}
	}
}

// A Spotify ID adds exact-ID conditions bound as a new parameter; the raw ID
// never appears in the SQL.
func TestAdminSearchSpotifyID(t *testing.T) {
	ctx := context.Background()
	trackArtist := "EXISTS (SELECT 1 FROM track_artists ta WHERE ta.track_id = t.id AND ta.artist_id = "
	artistTrack := "EXISTS (SELECT 1 FROM track_artists ta WHERE ta.artist_id = a.id AND ta.track_id = "
	artistAlbum := "WHERE ta.artist_id = a.id AND t.album_id = "
	cases := []struct {
		name      string
		run       func(q *Queries) error
		argIdx    int
		want, not []string
	}{
		{"history bare", func(q *Queries) error {
			_, err := q.AdminHistory(ctx, 50, time.Time{}, " "+testID+" ")
			return err
		}, 3, []string{"t.id = $4", trackArtist + "$4)", "t.album_id = $4"}, nil},
		{"history album url", func(q *Queries) error {
			_, err := q.AdminHistory(ctx, 50, time.Time{}, "https://open.spotify.com/album/"+testID+"?si=x")
			return err
		}, 3, []string{"t.album_id = $4"}, []string{"t.id = $4", trackArtist}},
		{"tracks bare", func(q *Queries) error {
			_, _, err := q.AdminTracks(ctx, testID, "", "", "", 50, 0)
			return err
		}, 1, []string{"t.id = $2", trackArtist + "$2)", "t.album_id = $2", "t.isrc ILIKE $1"}, nil},
		{"tracks artist uri + status", func(q *Queries) error {
			_, _, err := q.AdminTracks(ctx, "spotify:artist:"+testID, "failed", "", "", 50, 0)
			return err
		}, 1, []string{trackArtist + "$2)", "COALESCE(tf.status, 'pending') = $3"}, []string{"t.id = $2", "t.album_id = $2"}},
		{"artists bare", func(q *Queries) error {
			_, _, err := q.AdminArtists(ctx, testID, "", "", 50, 0)
			return err
		}, 1, []string{"a.id = $2", artistTrack + "$2)", artistAlbum + "$2)"}, nil},
		{"artists track url", func(q *Queries) error {
			_, _, err := q.AdminArtists(ctx, "https://open.spotify.com/intl-fr/track/"+testID, "", "", 50, 0)
			return err
		}, 1, []string{artistTrack + "$2)"}, []string{"a.id = $2", artistAlbum}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := &captureDB{}
			if err := tc.run(&Queries{db: db}); !errors.Is(err, errCaptured) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(db.sql, testID) {
				t.Errorf("ID spliced into SQL:\n%s", db.sql)
			}
			if len(db.args) <= tc.argIdx || db.args[tc.argIdx] != testID {
				t.Errorf("args = %v, want ID at index %d", db.args, tc.argIdx)
			}
			if !strings.Contains(db.sql, "search_fold(") {
				t.Errorf("name match dropped:\n%s", db.sql)
			}
			for _, w := range tc.want {
				if !strings.Contains(db.sql, w) {
					t.Errorf("sql lacks %q:\n%s", w, db.sql)
				}
			}
			for _, w := range tc.not {
				if strings.Contains(db.sql, w) {
					t.Errorf("sql has %q:\n%s", w, db.sql)
				}
			}
		})
	}
}

// A plain name search adds no ID condition and no extra parameter.
func TestAdminSearchNoIDCondition(t *testing.T) {
	ctx := context.Background()
	runs := []struct {
		name  string
		run   func(q *Queries)
		nargs int
	}{
		{"history", func(q *Queries) { _, _ = q.AdminHistory(ctx, 50, time.Time{}, "Sơn Tùng") }, 3},
		{"tracks", func(q *Queries) { _, _, _ = q.AdminTracks(ctx, "spotify:track:short", "", "", "", 50, 0) }, 1},
		{"artists", func(q *Queries) { _, _, _ = q.AdminArtists(ctx, testID[:21], "", "", 50, 0) }, 1},
	}
	for _, tc := range runs {
		db := &captureDB{}
		tc.run(&Queries{db: db})
		for _, bad := range []string{"t.id = $", "a.id = $", "t.album_id = $", "ta.artist_id = $", "ta.track_id = $"} {
			if strings.Contains(db.sql, bad) {
				t.Errorf("%s: unexpected ID condition %q:\n%s", tc.name, bad, db.sql)
			}
		}
		if len(db.args) != tc.nargs {
			t.Errorf("%s: args = %v, want %d", tc.name, db.args, tc.nargs)
		}
	}
}
