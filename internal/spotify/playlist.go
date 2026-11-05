package spotify

import (
	"context"
	"net/url"

	"github.com/baoopn/spotify-tracker/internal/domain"
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
	var pr playlistResponse
	found, err := m.getJSON(ctx, "playlist "+id,
		"/playlists/"+url.PathEscape(id)+"?market=from_token&additional_types=track", &pr)
	if err != nil || !found {
		return domain.Playlist{ID: id}, err
	}
	return playlistFromResponse(pr), nil
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
