package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/baoopn/spotify-tracker/internal/observability"
)

// MaxPlaylistItems is the most items one PUT /playlists/{id}/items accepts.
const MaxPlaylistItems = 100

// CreatePlaylist creates a playlist owned by the authorized user
// (POST /me/playlists) and returns its ID and open.spotify.com URL. Needs
// playlist-modify-public (public) or playlist-modify-private.
func (m *Manager) CreatePlaylist(ctx context.Context, name, description string, public bool) (id, link string, err error) {
	var resp struct {
		ID           string `json:"id"`
		ExternalURLs struct {
			Spotify string `json:"spotify"`
		} `json:"external_urls"`
	}
	body := map[string]any{"name": name, "public": public, "description": description}
	if err := m.sendJSON(ctx, http.MethodPost, "create playlist", "/me/playlists", body, &resp); err != nil {
		return "", "", err
	}
	if resp.ID == "" {
		return "", "", errors.New("spotify: create playlist: response has no id")
	}
	link = resp.ExternalURLs.Spotify
	if link == "" {
		link = "https://open.spotify.com/playlist/" + url.PathEscape(resp.ID)
	}
	return resp.ID, link, nil
}

// ReplacePlaylistItems replaces every item of a playlist with uris
// (PUT /playlists/{id}/items). Idempotent; at most MaxPlaylistItems URIs.
func (m *Manager) ReplacePlaylistItems(ctx context.Context, id string, uris []string) error {
	if len(uris) > MaxPlaylistItems {
		return fmt.Errorf("spotify: replace playlist items: %d uris, at most %d allowed", len(uris), MaxPlaylistItems)
	}
	if uris == nil {
		uris = []string{}
	}
	return m.sendJSON(ctx, http.MethodPut, "replace playlist items",
		"/playlists/"+url.PathEscape(id)+"/items", map[string]any{"uris": uris}, nil)
}

// sendJSON sends body as JSON to a Web API path with the stored credentials
// and decodes a 2xx response into v (when v is not nil). Like getJSON it
// retries once with a forced token refresh on 401. A 403 is returned as a
// wrapped ErrForbidden and a 429 as *RateLimitError.
func (m *Manager) sendJSON(ctx context.Context, method, what, path string, body, v any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("spotify: encode %s: %w", what, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := m.token(ctx, attempt > 0)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, m.oauth.api()+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := m.oauth.client().Do(req)
		if err != nil {
			return fmt.Errorf("spotify: %s: %w", what, err)
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		observability.UpstreamRequests.WithLabelValues("spotify", strconv.Itoa(resp.StatusCode)).Inc()

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if v != nil && len(bytes.TrimSpace(respBody)) > 0 {
				if err := json.Unmarshal(respBody, v); err != nil {
					return fmt.Errorf("spotify: decode %s: %w", what, err)
				}
			}
			return nil
		case resp.StatusCode == http.StatusUnauthorized:
			continue
		case resp.StatusCode == http.StatusForbidden:
			return fmt.Errorf("%w: %s: %s", ErrForbidden, what, truncate(respBody))
		case resp.StatusCode == http.StatusTooManyRequests:
			return rateLimitFrom(&http.Response{Header: resp.Header, Body: io.NopCloser(bytes.NewReader(respBody))})
		default:
			return fmt.Errorf("spotify: %s failed (%d): %s", what, resp.StatusCode, truncate(respBody))
		}
	}
	return fmt.Errorf("spotify: unauthorized: %s", what)
}
