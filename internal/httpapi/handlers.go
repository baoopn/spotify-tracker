package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type listResponse[T any] struct {
	Data       []T    `json:"data"`
	Range      string `json:"range,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type itemResponse[T any] struct {
	Data  T      `json:"data"`
	Range string `json:"range,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: msg}})
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.log.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), logging.Err(err))
	writeError(w, http.StatusInternalServerError, "internal", "internal server error")
}

func parseLimit(r *http.Request) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return defaultLimit, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > maxLimit {
		return 0, fmt.Errorf("limit must be an integer between 1 and %d", maxLimit)
	}
	return n, nil
}

func parseRange(r *http.Request) (domain.Range, error) {
	rg, ok := domain.ParseRange(r.URL.Query().Get("range"), time.Now().UTC())
	if !ok {
		return rg, errors.New("range must be one of 7d, 30d, 90d, 180d, 365d, all")
	}
	return rg, nil
}

// GET /api/v1/now-playing
func (h *Handler) nowPlaying(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	body, err := h.nowPlayingJSON(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"data":%s}`+"\n", body) //nolint:gosec // JSON produced by our own code
}

// nowPlayingJSON returns the current snapshot as JSON: the Redis copy kept
// by the tracker, else the database row, else null (nothing played yet).
func (h *Handler) nowPlayingJSON(ctx context.Context) ([]byte, error) {
	if body, err := h.rdb.Get(ctx, redisx.KeyNowPlaying).Bytes(); err == nil && json.Valid(body) {
		return body, nil
	}
	np, err := h.store.NowPlaying(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return []byte("null"), nil
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(np)
}

// GET /api/v1/now-playing/stream — Server-Sent Events of now-playing updates.
func (h *Handler) nowPlayingStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	ctx := r.Context()
	sub := h.rdb.Subscribe(ctx, redisx.ChannelPlayback)
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil {
		h.internalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Send the headers now: until the first flush nothing reaches the client,
	// which would treat the stream as dead and fall back to polling.
	flusher.Flush()

	send := func(event string, data []byte) bool {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// Always start with the current snapshot (null when nothing has played),
	// also when the Redis copy has expired because nothing is playing.
	body, err := h.nowPlayingJSON(ctx)
	if err != nil {
		h.log.WarnContext(ctx, "now-playing snapshot for stream failed", logging.Err(err))
		body = []byte("null")
	}
	if !send("now-playing", body) {
		return
	}

	ch := sub.Channel()
	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok || !send("now-playing", []byte(msg.Payload)) {
				return
			}
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// GET /api/v1/history?limit=20&cursor=<RFC3339 played_at>
func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	var before time.Time
	if c := r.URL.Query().Get("cursor"); c != "" {
		before, err = time.Parse(time.RFC3339Nano, c)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor must be an RFC 3339 timestamp")
			return
		}
	}
	items, err := h.store.History(r.Context(), limit, before)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	resp := listResponse[domain.HistoryEntry]{Data: items}
	if len(items) == limit {
		resp.NextCursor = items[len(items)-1].PlayedAt.UTC().Format(time.RFC3339Nano)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/v1/tracks/{id}
func (h *Handler) trackDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" || len(id) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid track id")
		return
	}
	d, err := h.store.TrackDetail(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if d.Features == nil || d.Features.Status == domain.EnrichmentPending {
		w.Header().Set(headerNoStore, "1")
	}
	writeJSON(w, http.StatusOK, itemResponse[domain.TrackDetail]{Data: d})
}

// GET /api/v1/stats/summary?range=30d
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	rg, err := parseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error())
		return
	}
	s, err := h.store.Summary(r.Context(), rg)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, itemResponse[domain.Summary]{Data: s, Range: rg.Name})
}

// GET /api/v1/stats/top-tracks?range=30d&limit=20
func (h *Handler) topTracks(w http.ResponseWriter, r *http.Request) {
	rg, limit, ok := h.rangeAndLimit(w, r)
	if !ok {
		return
	}
	items, err := h.store.TopTracks(r.Context(), rg, limit)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[domain.TopTrack]{Data: items, Range: rg.Name})
}

// GET /api/v1/stats/top-artists?range=30d&limit=20
func (h *Handler) topArtists(w http.ResponseWriter, r *http.Request) {
	rg, limit, ok := h.rangeAndLimit(w, r)
	if !ok {
		return
	}
	items, err := h.store.TopArtists(r.Context(), rg, limit)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[domain.TopArtist]{Data: items, Range: rg.Name})
}

// GET /api/v1/stats/genres?range=30d&limit=20
func (h *Handler) genres(w http.ResponseWriter, r *http.Request) {
	rg, limit, ok := h.rangeAndLimit(w, r)
	if !ok {
		return
	}
	items, err := h.store.Genres(r.Context(), rg, limit)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[domain.GenreStat]{Data: items, Range: rg.Name})
}

// GET /api/v1/stats/audio-profile?range=30d
func (h *Handler) audioProfile(w http.ResponseWriter, r *http.Request) {
	rg, err := parseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error())
		return
	}
	p, err := h.store.AudioProfile(r.Context(), rg)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, itemResponse[domain.AudioProfile]{Data: p, Range: rg.Name})
}

func (h *Handler) rangeAndLimit(w http.ResponseWriter, r *http.Request) (domain.Range, int, bool) {
	rg, err := parseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error())
		return rg, 0, false
	}
	limit, err := parseLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return rg, 0, false
	}
	return rg, limit, true
}

// GET /api/v1/playlists/recent?limit=5 — playlists recently played from.
// Only public playlists are listed unless ShowPrivatePlaylists is set.
func (h *Handler) recentPlaylists(w http.ResponseWriter, r *http.Request) {
	limit, err := intQuery(r, "limit", 5, 1, 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	items, err := h.store.RecentPlaylists(r.Context(), limit, h.opts.ShowPrivatePlaylists)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[domain.RecentPlaylist]{Data: items})
}

func intQuery(r *http.Request, name string, def, lo, hi int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, lo, hi)
	}
	return n, nil
}
