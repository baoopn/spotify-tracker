package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

type fakeTopStore struct {
	rows        map[string][]domain.TopPlaylistRecord
	limits      map[string]int
	trackCalls  []time.Time
	tracksByKey map[int64][]domain.TopPlaylistTrack
}

func (f *fakeTopStore) PublishedTopPlaylists(_ context.Context, period string, limit int) ([]domain.TopPlaylistRecord, error) {
	f.limits[period] = limit
	rows := f.rows[period]
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (f *fakeTopStore) TopPlaylistTracks(_ context.Context, _ string, start time.Time) ([]domain.TopPlaylistTrack, error) {
	f.trackCalls = append(f.trackCalls, start)
	return f.tracksByKey[start.Unix()], nil
}

func TestTopPlaylistsResponse(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	pub := time.Date(2026, 11, 9, 5, 1, 12, 0, time.UTC)
	// DST ended on Sun 1 Nov 2026, so the two weeks have different offsets.
	newest := time.Date(2026, 11, 2, 0, 0, 0, 0, ny)
	older := time.Date(2026, 10, 26, 0, 0, 0, 0, ny)
	row := func(start time.Time) domain.TopPlaylistRecord {
		return domain.TopPlaylistRecord{Period: "week", PeriodStart: start.UTC(), PeriodEnd: start.AddDate(0, 0, 7).UTC(),
			TimeZone: "America/New_York", Name: topplaylists.Name(topplaylists.Week, start), Status: domain.TopPlaylistCreated,
			SpotifyPlaylistID: "pl" + start.Format("0102"), SpotifyURL: "https://open.spotify.com/playlist/x",
			TrackCount: 2, TotalPlays: 9, PublishedAt: &pub}
	}
	st := &fakeTopStore{
		limits: map[string]int{},
		rows:   map[string][]domain.TopPlaylistRecord{"week": {row(newest), row(older)}},
		tracksByKey: map[int64][]domain.TopPlaylistTrack{newest.Unix(): {
			{Position: 1, Plays: 5, Track: domain.TrackView{Track: domain.Track{ID: "t1", Name: "One"}, Artists: []domain.Artist{}}},
			{Position: 2, Plays: 4, Track: domain.TrackView{Track: domain.Track{ID: "t2", Name: "Two"}, Artists: []domain.Artist{}}},
		}},
	}
	h := &Handler{top: st, log: slog.New(slog.DiscardHandler), opts: Options{
		TopPlaylists: topplaylists.Config{Periods: []topplaylists.Period{topplaylists.Week, topplaylists.Year}, Location: ny},
	}}
	rec := httptest.NewRecorder()
	h.topPlaylists(rec, httptest.NewRequest(http.MethodGet, "/api/v1/playlists/top?limit=3", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	var body struct {
		Data struct {
			TimeZone string                                  `json:"time_zone"`
			Periods  map[string][]map[string]json.RawMessage `json:"periods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.TimeZone != "America/New_York" {
		t.Errorf("time_zone = %q", body.Data.TimeZone)
	}
	for _, k := range []string{"week", "month", "year"} {
		v, ok := body.Data.Periods[k]
		if !ok || v == nil {
			t.Errorf("period %q missing or null", k)
		}
	}
	if len(body.Data.Periods["month"]) != 0 || len(body.Data.Periods["year"]) != 0 {
		t.Errorf("month/year should be empty: %s", rec.Body)
	}
	if st.limits["week"] != 4 || st.limits["year"] != 4 {
		t.Errorf("limits = %v, want newest + 3 older", st.limits)
	}
	if _, queried := st.limits["month"]; queried {
		t.Error("disabled kind was queried")
	}
	weeks := body.Data.Periods["week"]
	if len(weeks) != 2 {
		t.Fatalf("weeks = %d", len(weeks))
	}
	for _, f := range []string{"period", "period_start", "period_end", "label", "name", "spotify_id", "spotify_url",
		"track_count", "total_plays", "published_at", "tracks"} {
		if _, ok := weeks[0][f]; !ok {
			t.Errorf("field %q missing", f)
		}
	}
	str := func(m map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(m[k], &s)
		return s
	}
	if got := str(weeks[0], "period_start"); got != "2026-11-02T00:00:00-05:00" {
		t.Errorf("period_start = %q (want RFC 3339 in the zone)", got)
	}
	if got := str(weeks[1], "period_start"); got != "2026-10-26T00:00:00-04:00" {
		t.Errorf("older period_start = %q", got)
	}
	if got := str(weeks[0], "label"); got != "Week of Nov 2, 2026" {
		t.Errorf("label = %q", got)
	}
	if got := str(weeks[0], "name"); got != "Top 50 · Week of Nov 2, 2026" {
		t.Errorf("name = %q", got)
	}
	if got := str(weeks[0], "published_at"); got != "2026-11-09T05:01:12Z" {
		t.Errorf("published_at = %q", got)
	}
	var tracks0, tracks1 []struct {
		Position int   `json:"position"`
		Plays    int64 `json:"plays"`
		Track    struct {
			ID string `json:"id"`
		} `json:"track"`
	}
	_ = json.Unmarshal(weeks[0]["tracks"], &tracks0)
	_ = json.Unmarshal(weeks[1]["tracks"], &tracks1)
	if len(tracks0) != 2 || tracks0[0].Position != 1 || tracks0[0].Plays != 5 || tracks0[0].Track.ID != "t1" {
		t.Errorf("newest tracks = %s", weeks[0]["tracks"])
	}
	if string(weeks[1]["tracks"]) != "[]" || len(tracks1) != 0 {
		t.Errorf("older tracks = %s, want []", weeks[1]["tracks"])
	}
	if len(st.trackCalls) != 1 {
		t.Errorf("tracks loaded %d times, want only for the newest", len(st.trackCalls))
	}
}

func TestTopPlaylistsDisabledAndLimit(t *testing.T) {
	h := &Handler{top: &fakeTopStore{limits: map[string]int{}}, log: slog.New(slog.DiscardHandler)}
	rec := httptest.NewRecorder()
	h.topPlaylists(rec, httptest.NewRequest(http.MethodGet, "/api/v1/playlists/top", nil))
	want := `{"data":{"time_zone":"UTC","periods":{"month":[],"week":[],"year":[]}}}` + "\n"
	if rec.Code != 200 || rec.Body.String() != want {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{"0", "25", "x"} {
		rec = httptest.NewRecorder()
		h.topPlaylists(rec, httptest.NewRequest(http.MethodGet, "/api/v1/playlists/top?limit="+bad, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: got %d", bad, rec.Code)
		}
	}
}

func TestTopPlaylistsExcludeHidden(t *testing.T) {
	week := func(start time.Time, hidden bool) domain.TopPlaylistRecord {
		return domain.TopPlaylistRecord{Period: "week", PeriodStart: start, PeriodEnd: start.AddDate(0, 0, 7),
			TimeZone: "UTC", Name: topplaylists.Name(topplaylists.Week, start), Status: domain.TopPlaylistCreated,
			SpotifyPlaylistID: "pl" + start.Format("0102"), Hidden: hidden}
	}
	hiddenStart := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	shownStart := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	st := &fakeTopStore{limits: map[string]int{},
		rows: map[string][]domain.TopPlaylistRecord{"week": {week(hiddenStart, true), week(shownStart, false)}}}
	h := &Handler{top: st, log: slog.New(slog.DiscardHandler), opts: Options{
		TopPlaylists: topplaylists.Config{Periods: []topplaylists.Period{topplaylists.Week}, Location: time.UTC},
	}}
	rec := httptest.NewRecorder()
	h.topPlaylists(rec, httptest.NewRequest(http.MethodGet, "/api/v1/playlists/top", nil))
	var body struct {
		Data struct {
			Periods map[string][]struct {
				SpotifyID string `json:"spotify_id"`
			} `json:"periods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	weeks := body.Data.Periods["week"]
	if len(weeks) != 1 || weeks[0].SpotifyID != "pl1026" {
		t.Fatalf("weeks = %+v, want only the visible playlist", weeks)
	}
	// The newest visible entry carries the tracks, not the hidden one.
	if len(st.trackCalls) != 1 || !st.trackCalls[0].Equal(shownStart) {
		t.Errorf("tracks loaded for %v", st.trackCalls)
	}
}
