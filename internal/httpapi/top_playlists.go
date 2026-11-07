package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

// topPlaylistStore reads published top playlists (implemented by *store.Store).
type topPlaylistStore interface {
	PublishedTopPlaylists(ctx context.Context, period string, limit int) ([]domain.TopPlaylistRecord, error)
	TopPlaylistTracks(ctx context.Context, period string, start time.Time) ([]domain.TopPlaylistTrack, error)
}

// topPlaylist is one published playlist in GET /api/v1/playlists/top.
type topPlaylist struct {
	Period      string                    `json:"period"`
	PeriodStart time.Time                 `json:"period_start"`
	PeriodEnd   time.Time                 `json:"period_end"`
	Label       string                    `json:"label"`
	Name        string                    `json:"name"`
	SpotifyID   string                    `json:"spotify_id"`
	SpotifyURL  string                    `json:"spotify_url"`
	TrackCount  int                       `json:"track_count"`
	TotalPlays  int64                     `json:"total_plays"`
	PublishedAt *time.Time                `json:"published_at"`
	Tracks      []domain.TopPlaylistTrack `json:"tracks"`
}

type topPlaylists struct {
	TimeZone string                   `json:"time_zone"`
	Periods  map[string][]topPlaylist `json:"periods"`
}

// GET /api/v1/playlists/top?limit=6 — the auto-generated "Top 50" playlists,
// newest first per period kind. limit is how many older entries to list
// besides the newest one; only the newest entry carries its tracks.
func (h *Handler) topPlaylists(w http.ResponseWriter, r *http.Request) {
	older, err := intQuery(r, "limit", 6, 1, 24)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	cfg := h.opts.TopPlaylists
	loc := cfg.Location
	if loc == nil {
		loc = time.UTC
	}
	resp := topPlaylists{TimeZone: loc.String(), Periods: map[string][]topPlaylist{}}
	for _, p := range topplaylists.AllPeriods {
		items := []topPlaylist{}
		if cfg.Has(p) {
			items, err = h.topPlaylistsOf(r.Context(), p, older+1, loc)
			if err != nil {
				h.internalError(w, r, err)
				return
			}
		}
		resp.Periods[string(p)] = items
	}
	writeJSON(w, http.StatusOK, itemResponse[topPlaylists]{Data: resp})
}

func (h *Handler) topPlaylistsOf(ctx context.Context, p topplaylists.Period, limit int, loc *time.Location) ([]topPlaylist, error) {
	rows, err := h.top.PublishedTopPlaylists(ctx, string(p), limit)
	if err != nil {
		return nil, err
	}
	out := make([]topPlaylist, 0, len(rows))
	for i, row := range rows {
		zone := loc
		if l, err := time.LoadLocation(row.TimeZone); err == nil && row.TimeZone != "" {
			zone = l // label in the zone the period was generated in
		}
		start := row.PeriodStart.In(zone)
		item := topPlaylist{
			Period: row.Period, PeriodStart: start, PeriodEnd: row.PeriodEnd.In(zone),
			Label: topplaylists.Label(p, start), Name: row.Name,
			SpotifyID: row.SpotifyPlaylistID, SpotifyURL: row.SpotifyURL,
			TrackCount: row.TrackCount, TotalPlays: row.TotalPlays,
			Tracks: []domain.TopPlaylistTrack{},
		}
		if row.PublishedAt != nil {
			t := row.PublishedAt.UTC()
			item.PublishedAt = &t
		}
		if i == 0 {
			tracks, err := h.top.TopPlaylistTracks(ctx, row.Period, row.PeriodStart)
			if err != nil {
				return nil, err
			}
			if tracks != nil {
				item.Tracks = tracks
			}
		}
		out = append(out, item)
	}
	return out, nil
}
