package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/observability"
)

// ErrQueueForbidden means Spotify refused the queue endpoint (403): the
// token lacks user-read-playback-state, or the app may not use it.
var ErrQueueForbidden = errors.New("spotify: queue not available (403)")

// trackIDPattern is a Spotify track id: 22 base62 characters.
var trackIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

type queueItem struct {
	Type    string `json:"type"`
	IsLocal bool   `json:"is_local"`
	trackObject
}

type queueResponse struct {
	// currently_playing is ignored: the poller reads it separately.
	Queue []queueItem `json:"queue"`
}

// Queue returns the first limit tracks of the user's play queue, in queue
// order: episodes, local files, invalid entries and repeats are skipped. An
// empty queue (or HTTP 204) gives an empty list.
func (p *Player) Queue(ctx context.Context, limit int) ([]domain.CatalogEntry, error) {
	force := false
	for attempt := 0; attempt < 2; attempt++ {
		out, unauthorized, err := p.queue(ctx, force, limit)
		if unauthorized {
			force = true
			continue
		}
		return out, err
	}
	return nil, errors.New("spotify: unauthorized after token refresh")
}

func (p *Player) queue(ctx context.Context, force bool, limit int) ([]domain.CatalogEntry, bool, error) {
	tok, err := p.tokens.AccessToken(ctx, force)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiBase+"/me/player/queue", nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("spotify: queue: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	observability.UpstreamRequests.WithLabelValues("spotify", strconv.Itoa(resp.StatusCode)).Inc()

	switch {
	case resp.StatusCode == http.StatusNoContent:
		return []domain.CatalogEntry{}, false, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, true, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, false, rateLimitFrom(resp)
	case resp.StatusCode == http.StatusForbidden:
		return nil, false, ErrQueueForbidden
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, false, fmt.Errorf("spotify: queue failed (%d): %s", resp.StatusCode, truncate(body))
	}

	var qr queueResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&qr); err != nil {
		return nil, false, fmt.Errorf("spotify: decode queue: %w", err)
	}
	return queueEntries(qr.Queue, limit), false, nil
}

// queueEntries keeps playable tracks in queue order, without repeats, up to
// limit (limit counts tracks, not queue positions).
func queueEntries(items []queueItem, limit int) []domain.CatalogEntry {
	out := []domain.CatalogEntry{}
	seen := map[string]bool{}
	for i := range items {
		if len(out) >= limit {
			break
		}
		it := &items[i]
		if (it.Type != "" && it.Type != "track") || it.IsLocal || !trackIDPattern.MatchString(it.ID) ||
			it.Name == "" || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		out = append(out, it.entry())
	}
	return out
}
