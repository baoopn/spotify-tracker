package spotify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

func TestAlbumDetailsParses(t *testing.T) {
	long := strings.Repeat("é", maxCopyrightLen+10)
	extra := strings.Repeat(`,{"text":"extra","type":"c"}`, maxCopyrights+3)
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/albums/al1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"al1","album_type":"COMPILATION","total_tracks":12,
			"release_date_precision":"month","external_ids":{"upc":" 00602508 "},
			"copyrights":[{"text":"  © 2020 Label ","type":"c"},{"text":"  ","type":"P"},
				{"text":"` + long + `","type":"p"}` + extra + `],
			"tracks":{"next":"https://api.spotify.com/v1/albums/al1/tracks?offset=50","items":[
				{"id":"t1","track_number":1,"disc_number":1},
				{"id":"t2","track_number":2,"disc_number":1,"linked_from":{"id":"old2"}},
				{"id":"t3","track_number":0,"disc_number":2},
				{"id":"","track_number":4,"disc_number":1}]}}`))
	})
	d, err := m.AlbumDetails(context.Background(), "al1")
	if err != nil || d == nil {
		t.Fatalf("got %v, %v", d, err)
	}
	if d.AlbumType != "compilation" || d.ReleaseDatePrecision != "month" || d.UPC != "00602508" ||
		d.TotalTracks == nil || *d.TotalTracks != 12 {
		t.Errorf("fields mapped wrong: %+v", d)
	}
	if len(d.Copyrights) != maxCopyrights {
		t.Fatalf("copyrights = %d, want %d", len(d.Copyrights), maxCopyrights)
	}
	if d.Copyrights[0] != (domain.Copyright{Text: "© 2020 Label", Type: "C"}) {
		t.Errorf("copyright 0 = %+v", d.Copyrights[0])
	}
	if c := d.Copyrights[1]; c.Type != "P" || len([]rune(c.Text)) != maxCopyrightLen {
		t.Errorf("copyright 1 not capped: type %q, %d runes", c.Type, len([]rune(c.Text)))
	}
	one, two := int32(1), int32(2)
	want := []domain.AlbumTrackNumber{
		{TrackID: "t1", TrackNumber: &one, DiscNumber: &one},
		{TrackID: "t2", TrackNumber: &two, DiscNumber: &one},
		{TrackID: "old2", TrackNumber: &two, DiscNumber: &one},
		{TrackID: "t3", TrackNumber: nil, DiscNumber: &two},
	}
	if !reflect.DeepEqual(d.Tracks, want) {
		t.Errorf("tracks = %+v", d.Tracks)
	}
}

func TestAlbumDetailsEmptyCopyrights(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"al1","album_type":"ep"}`))
	})
	d, err := m.AlbumDetails(context.Background(), "al1")
	if err != nil || d == nil || d.Copyrights == nil || len(d.Copyrights) != 0 || d.AlbumType != "" || d.TotalTracks != nil {
		t.Fatalf("got %+v, %v", d, err)
	}
}

func TestAlbumDetailsNotFound(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		if d, err := m.AlbumDetails(context.Background(), "al1"); d != nil || err != nil {
			t.Fatalf("%d: got %v, %v", status, d, err)
		}
	}
}

// A 403 is logged at warn (an app-wide refusal must stand out), a 404 at debug.
func TestRefusedLookupLogLevels(t *testing.T) {
	for _, tc := range []struct {
		status int
		level  string
	}{{http.StatusForbidden, "level=WARN"}, {http.StatusNotFound, "level=DEBUG"}} {
		m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
		var buf bytes.Buffer
		m.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		_, _ = m.AlbumDetails(context.Background(), "al1")
		_, _ = m.ArtistProfile(context.Background(), "a1")
		if got := strings.Count(buf.String(), tc.level); got != 2 {
			t.Errorf("%d: %d lines at %s:\n%s", tc.status, got, tc.level, buf.String())
		}
	}
}

func TestAlbumDetailsErrors(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := m.AlbumDetails(context.Background(), "al1")
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
		t.Fatalf("429: got %v", err)
	}

	m, _ = writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) })
	_, err = m.AlbumDetails(context.Background(), "al1")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadRequest || !se.Permanent() {
		t.Fatalf("400: got %v", err)
	}
}

func TestAlbumDetailsEscapesID(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/albums/a%2Fb%3Fc" {
			t.Errorf("escaped path = %s", r.URL.EscapedPath())
		}
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := m.AlbumDetails(context.Background(), "a/b?c"); err != nil {
		t.Fatal(err)
	}
}
