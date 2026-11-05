package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/observability"
)

type playlistResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Public        *bool  `json:"public"`
	Collaborative bool   `json:"collaborative"`
	SnapshotID    string `json:"snapshot_id"`
	URI           string `json:"uri"`
	ExternalURLs  struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	Owner struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"owner"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	// Spotify renamed "tracks" to "items" in 2026; accept both.
	Tracks *struct {
		Total int32 `json:"total"`
	} `json:"tracks"`
	Items *struct {
		Total int32 `json:"total"`
	} `json:"items"`
}

// Playlist fetches playlist metadata with the stored credentials. Playlists
// Spotify refuses to show (403/404 — e.g. Spotify-generated mixes for
// Development Mode apps, or private playlists without the scope) are returned
// with Available=false and no error.
func (m *Manager) Playlist(ctx context.Context, id string) (domain.Playlist, error) {
	out := domain.Playlist{ID: id}
	for attempt := 0; attempt < 2; attempt++ {
		tok, _, err := m.AccessToken(ctx, attempt > 0)
		if err != nil {
			return out, err
		}
		u := m.oauth.api() + "/playlists/" + url.PathEscape(id) + "?market=from_token&additional_types=track"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := m.oauth.client().Do(req)
		if err != nil {
			return out, fmt.Errorf("spotify: get playlist: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		observability.UpstreamRequests.WithLabelValues("spotify", strconv.Itoa(resp.StatusCode)).Inc()

		switch resp.StatusCode {
		case http.StatusOK:
			var pr playlistResponse
			if err := json.Unmarshal(body, &pr); err != nil {
				return out, fmt.Errorf("spotify: decode playlist: %w", err)
			}
			return playlistFromResponse(pr), nil
		case http.StatusUnauthorized:
			continue
		case http.StatusForbidden, http.StatusNotFound:
			out.Available = false
			return out, nil
		case http.StatusTooManyRequests:
			return out, rateLimitFrom(&http.Response{Header: resp.Header, Body: io.NopCloser(bytes.NewReader(body))})
		default:
			return out, fmt.Errorf("spotify: get playlist failed (%d): %s", resp.StatusCode, truncate(body))
		}
	}
	return out, fmt.Errorf("spotify: unauthorized fetching playlist %s", id)
}

func playlistFromResponse(pr playlistResponse) domain.Playlist {
	p := domain.Playlist{
		ID: pr.ID, Available: true, Name: pr.Name, Description: pr.Description,
		OwnerID: pr.Owner.ID, OwnerName: pr.Owner.DisplayName, ExternalURL: pr.ExternalURLs.Spotify,
		URI: pr.URI, Public: pr.Public, Collaborative: pr.Collaborative, SnapshotID: pr.SnapshotID,
	}
	if len(pr.Images) > 0 {
		p.CoverImageURL = pr.Images[0].URL
	}
	switch {
	case pr.Items != nil:
		t := pr.Items.Total
		p.TracksTotal = &t
	case pr.Tracks != nil:
		t := pr.Tracks.Total
		p.TracksTotal = &t
	}
	return p
}
