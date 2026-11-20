package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/baoopn/spotify-tracker/internal/observability"
)

// getJSON GETs a Web API path with the stored credentials and decodes the
// response into v. It retries once with a forced token refresh on 401.
// found is false (with no error) when Spotify answers 403 or 404 — content
// that is hidden from Development Mode apps or no longer exists.
func (m *Manager) getJSON(ctx context.Context, what, path string, v any) (found bool, err error) {
	code, err := m.getJSONCode(ctx, what, path, v)
	return code == http.StatusOK, err
}

// getJSONCode is getJSON that also tells a refusal (403) from a missing
// item (404): it returns 200, 403 or 404 with no error, or 0 with an error.
func (m *Manager) getJSONCode(ctx context.Context, what, path string, v any) (int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := m.token(ctx, attempt > 0)
		if err != nil {
			return 0, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.oauth.api()+path, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := m.oauth.client().Do(req)
		if err != nil {
			return 0, fmt.Errorf("spotify: get %s: %w", what, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		observability.UpstreamRequests.WithLabelValues("spotify", strconv.Itoa(resp.StatusCode)).Inc()

		switch resp.StatusCode {
		case http.StatusOK:
			if err := json.Unmarshal(body, v); err != nil {
				return 0, fmt.Errorf("spotify: decode %s: %w", what, err)
			}
			return http.StatusOK, nil
		case http.StatusUnauthorized:
			continue
		case http.StatusForbidden, http.StatusNotFound:
			return resp.StatusCode, nil
		case http.StatusTooManyRequests:
			return 0, rateLimitFrom(&http.Response{Header: resp.Header, Body: io.NopCloser(bytes.NewReader(body))})
		default:
			return 0, &StatusError{Code: resp.StatusCode,
				msg: fmt.Sprintf("spotify: get %s failed (%d): %s", what, resp.StatusCode, truncate(body))}
		}
	}
	return 0, fmt.Errorf("spotify: unauthorized fetching %s", what)
}

// logRefused logs a lookup that found nothing: a 403 at warn, since an
// app-wide refusal would otherwise go unnoticed, and a 404 at debug.
func (m *Manager) logRefused(ctx context.Context, what string, code int) {
	if m.log == nil {
		return
	}
	if code == http.StatusForbidden {
		m.log.WarnContext(ctx, "spotify refused lookup (403)", slog.String("what", what))
		return
	}
	m.log.DebugContext(ctx, "spotify lookup not found", slog.String("what", what), slog.Int("status", code))
}

// StatusError is an unexpected HTTP status from a Web API GET (not 401, 403,
// 404 or 429, which have their own handling).
type StatusError struct {
	Code int
	msg  string
}

func (e *StatusError) Error() string { return e.msg }

// Permanent reports a client error (4xx) that retrying the same request
// will not fix.
func (e *StatusError) Permanent() bool { return e.Code >= 400 && e.Code < 500 }
