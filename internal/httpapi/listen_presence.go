package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/logging"
)

// PresenceWriter records who is on /listen (implemented by
// *listen.PresenceStore).
type PresenceWriter interface {
	Touch(ctx context.Context, p listen.Presence) error
}

// POST /api/v1/listen/presence — {state, track_id}: the listener's state on
// the page. Stored in Redis for 90 s; nothing for an admin previewing.
func (h *Handler) listenPresence(w http.ResponseWriter, r *http.Request) {
	var in struct {
		State   string  `json:"state"`
		TrackID *string `json:"track_id"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	fields := map[string]string{}
	switch in.State {
	case listen.StateListening, listen.StateFollowing, listen.StateIdle:
	default:
		fields["state"] = "must be listening, following or idle"
	}
	if in.TrackID != nil && !listenTrackID.MatchString(*in.TrackID) {
		fields["track_id"] = "must be a Spotify track ID or null"
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	if l, ok := h.listener(r.Context()); ok {
		h.touchPresence(r, l, in.State, in.TrackID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// touchPresence stores the listener's presence. Failures are logged only:
// presence is a hint for the admin, never a reason to fail a request.
func (h *Handler) touchPresence(r *http.Request, l ListenerIdentity, state string, trackID *string) {
	pw := h.opts.Listen.Presence
	if pw == nil {
		return
	}
	device, browser := analytics.DeviceBrowser(r.UserAgent())
	err := pw.Touch(r.Context(), listen.Presence{ListenerID: l.ID, Email: l.Email, State: state, TrackID: trackID,
		Country: analytics.Country(r), Device: device, Browser: browser})
	if err != nil {
		h.log.WarnContext(r.Context(), "listen presence write failed", slog.Int64("listener_id", l.ID), logging.Err(err))
	}
}
