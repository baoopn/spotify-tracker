package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/baoopn/spotify-tracker/internal/observability"
)

// getJSON GETs a Web API path with the stored credentials and decodes the
// response into v. It retries once with a forced token refresh on 401.
// found is false (with no error) when Spotify answers 403 or 404 — content
// that is hidden from Development Mode apps or no longer exists.
func (m *Manager) getJSON(ctx context.Context, what, path string, v any) (found bool, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := m.token(ctx, attempt > 0)
		if err != nil {
			return false, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.oauth.api()+path, nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := m.oauth.client().Do(req)
		if err != nil {
			return false, fmt.Errorf("spotify: get %s: %w", what, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		observability.UpstreamRequests.WithLabelValues("spotify", strconv.Itoa(resp.StatusCode)).Inc()

		switch resp.StatusCode {
		case http.StatusOK:
			if err := json.Unmarshal(body, v); err != nil {
				return false, fmt.Errorf("spotify: decode %s: %w", what, err)
			}
			return true, nil
		case http.StatusUnauthorized:
			continue
		case http.StatusForbidden, http.StatusNotFound:
			return false, nil
		case http.StatusTooManyRequests:
			return false, rateLimitFrom(&http.Response{Header: resp.Header, Body: io.NopCloser(bytes.NewReader(body))})
		default:
			return false, fmt.Errorf("spotify: get %s failed (%d): %s", what, resp.StatusCode, truncate(body))
		}
	}
	return false, fmt.Errorf("spotify: unauthorized fetching %s", what)
}
