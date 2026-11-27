package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/logging"
)

// ListenOptions configures listen along (/api/v1/listen/*).
type ListenOptions struct {
	// Enabled mirrors LISTEN_ENABLED; when false /me and /audio answer 404.
	Enabled bool
	// ClerkPublishableKey lets the /listen page sign people in.
	ClerkPublishableKey string
	// Auth verifies the Clerk session and the listener (admin
	// ListenerMiddleware); nil answers 503 (Clerk is not configured).
	Auth func(http.Handler) http.Handler
	// Me serves GET /listen/me behind Auth (admin.ListenerMe).
	Me http.HandlerFunc
	// Audio finds audio files; nil answers reason storage_unconfigured.
	Audio AudioLocator
}

// AudioLocator finds the audio of a track (implemented by *listen.Locator).
type AudioLocator interface {
	Locate(ctx context.Context, trackID string, remaining time.Duration) (*listen.AudioURL, error)
}

// Reasons why GET /listen/audio has no URL.
const (
	listenIdle                = "idle"
	listenNotFound            = "not_found"
	listenStorageUnconfigured = "storage_unconfigured"
	listenUnavailable         = "unavailable"
)

type listenConfigBody struct {
	Enabled             bool    `json:"enabled"`
	ClerkPublishableKey *string `json:"clerk_publishable_key"`
}

// listenAudioBody is the audio of the track playing now. Reason is null
// exactly when URL is set.
type listenAudioBody struct {
	TrackID     *string    `json:"track_id"`
	URL         *string    `json:"url"`
	ExpiresAt   *time.Time `json:"expires_at"`
	ContentType *string    `json:"content_type"`
	Reason      *string    `json:"reason"`
}

// mountListen registers the listen-along routes (relative to /api/v1). They
// are not in v1Routes, so the MCP server never reaches them.
func (h *Handler) mountListen(r chi.Router) {
	o := h.opts.Listen
	r.Route("/listen", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
			})
		})
		r.Get("/config", h.listenConfig)
		r.Group(func(r chi.Router) {
			switch {
			case !o.Enabled:
				r.Use(func(http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						writeError(w, http.StatusNotFound, "listen_disabled", "listen along is not enabled")
					})
				})
			case o.Auth == nil || o.Me == nil:
				r.Use(func(http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						writeError(w, http.StatusServiceUnavailable, "listen_unavailable",
							"listen along needs Clerk (set CLERK_SECRET_KEY and CLERK_PUBLISHABLE_KEY)")
					})
				})
			default:
				r.Use(o.Auth)
			}
			r.Get("/me", func(w http.ResponseWriter, r *http.Request) { o.Me(w, r) })
			r.Get("/audio", h.listenAudio)
		})
	})
}

// GET /api/v1/listen/config — whether listen along is on, and the Clerk key
// for the sign-in form. Always 200.
func (h *Handler) listenConfig(w http.ResponseWriter, _ *http.Request) {
	o := h.opts.Listen
	body := listenConfigBody{Enabled: o.Enabled}
	if o.Enabled && o.ClerkPublishableKey != "" {
		pk := o.ClerkPublishableKey
		body.ClerkPublishableKey = &pk
	}
	writeJSON(w, http.StatusOK, itemResponse[listenConfigBody]{Data: body})
}

// GET /api/v1/listen/audio — a presigned URL for the audio of the track
// playing now (also while paused). The URL is never logged.
func (h *Handler) listenAudio(w http.ResponseWriter, r *http.Request) {
	np, pos, ok, err := h.currentPlayback(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	reason := func(v string) *string { return &v }
	var body listenAudioBody
	switch {
	case !ok:
		body.Reason = reason(listenIdle)
	case h.opts.Listen.Audio == nil:
		body.TrackID, body.Reason = &np.Track.ID, reason(listenStorageUnconfigured)
	default:
		body.TrackID = &np.Track.ID
		var remaining time.Duration
		if d := np.Track.DurationMS; d > 0 {
			remaining = time.Duration(d-pos) * time.Millisecond
		}
		u, err := h.opts.Listen.Audio.Locate(r.Context(), np.Track.ID, remaining)
		switch {
		case err != nil:
			// Not cached: the client asks again in 30 s.
			h.log.WarnContext(r.Context(), "listen audio lookup failed", slog.String("track_id", np.Track.ID), logging.Err(err))
			body.Reason = reason(listenUnavailable)
		case u == nil:
			body.Reason = reason(listenNotFound)
		default:
			exp := u.ExpiresAt.UTC()
			body.URL, body.ExpiresAt, body.ContentType = &u.URL, &exp, &u.ContentType
		}
	}
	writeJSON(w, http.StatusOK, itemResponse[listenAudioBody]{Data: body})
}
