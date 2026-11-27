package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/baoopn/spotify-tracker/internal/store"
)

// ListenPlayStore records and reads listen-along history (implemented by
// *store.Store).
type ListenPlayStore interface {
	RecordListenerProgress(ctx context.Context, listenerID int64, trackID string, deltaMS, durationMS int64) error
	ListenerHistory(ctx context.Context, listenerID int64, limit int, cursor string) ([]store.ListenPlay, *string, error)
}

const (
	maxProgressMS       = 20_000
	defaultListenLimit  = 50
	maxListenHistoryLim = 100
)

var listenTrackID = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

type fieldsError struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

func writeValidation(w http.ResponseWriter, fields map[string]string) {
	var body fieldsError
	body.Error.Code, body.Error.Message, body.Error.Fields = "validation_failed", "some fields are invalid", fields
	writeJSON(w, http.StatusUnprocessableEntity, body)
}

// decodeBody reads a small JSON body into v; false answers 400.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return false
	}
	return true
}

// listener returns the invited listener of the request (false for an
// admin previewing, or when history is off).
func (h *Handler) listener(ctx context.Context) (ListenerIdentity, bool) {
	o := h.opts.Listen
	if o == nil || o.Listener == nil {
		return ListenerIdentity{}, false
	}
	return o.Listener(ctx)
}

// POST /api/v1/listen/progress — {track_id, heard_ms}: audio time heard
// since the last report. Counted only for the track playing now; anything
// else answers 204 and stores nothing.
func (h *Handler) listenProgress(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TrackID string `json:"track_id"`
		HeardMS int64  `json:"heard_ms"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	fields := map[string]string{}
	if !listenTrackID.MatchString(in.TrackID) {
		fields["track_id"] = "must be a Spotify track ID"
	}
	if in.HeardMS < 1 || in.HeardMS > maxProgressMS {
		fields["heard_ms"] = "must be between 1 and " + strconv.Itoa(maxProgressMS)
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	l, ok := h.listener(r.Context())
	if !ok || h.opts.Listen.Plays == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	np, _, playing, err := h.currentPlayback(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if !playing || np.Track.ID != in.TrackID {
		w.WriteHeader(http.StatusNoContent) // a race at a track change: not an error
		return
	}
	if err := h.opts.Listen.Plays.RecordListenerProgress(r.Context(), l.ID, in.TrackID, in.HeardMS, np.Track.DurationMS); err != nil {
		h.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type historyResponse struct {
	Data       []store.ListenPlay `json:"data"`
	NextCursor *string            `json:"next_cursor"`
}

// GET /api/v1/listen/history?limit=&cursor= — the caller's own counted
// plays, newest first.
func (h *Handler) listenHistory(w http.ResponseWriter, r *http.Request) {
	limit, err := intQuery(r, "limit", defaultListenLimit, 1, maxListenHistoryLim)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	l, ok := h.listener(r.Context())
	if !ok || h.opts.Listen.Plays == nil {
		writeJSON(w, http.StatusOK, historyResponse{Data: []store.ListenPlay{}})
		return
	}
	plays, next, err := h.opts.Listen.Plays.ListenerHistory(r.Context(), l.ID, limit, r.URL.Query().Get("cursor"))
	if errors.Is(err, store.ErrBadCursor) {
		writeError(w, http.StatusBadRequest, "invalid_cursor", "invalid cursor")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if plays == nil {
		plays = []store.ListenPlay{}
	}
	writeJSON(w, http.StatusOK, historyResponse{Data: plays, NextCursor: next})
}
