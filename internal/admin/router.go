package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

// Mount registers /admin/api on r. auth may be nil when Clerk is not
// configured, in which case every protected route answers 503.
func Mount(r chi.Router, a *API, auth *Authenticator) {
	if auth != nil {
		auth.onForbidden = func(ctx context.Context, u User, ip string) {
			_ = a.Store.Audit(ctx, u.Email, "auth.forbidden", map[string]string{"user_id": u.ID}, ip)
		}
	}
	r.Route("/admin/api", func(r chi.Router) {
		r.Use(middleware.NoCache)
		r.Use(httprate.LimitBy(300, time.Minute, func(r *http.Request) (string, error) {
			return "admin:" + clientIP(r), nil
		}))
		r.Use(middleware.Timeout(30 * time.Second))

		r.Get("/config", a.config)
		r.Get("/spotify/callback", a.spotifyCallback)

		r.Group(func(r chi.Router) {
			if auth == nil {
				r.Use(func(http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						writeError(w, http.StatusServiceUnavailable, "admin_disabled",
							"admin dashboard is not configured (set CLERK_SECRET_KEY, CLERK_PUBLISHABLE_KEY and ADMIN_ALLOWED_EMAILS)")
					})
				})
			} else {
				r.Use(auth.Middleware)
			}
			r.Get("/auth/me", a.me)
			r.Get("/overview", a.overview)

			r.Get("/spotify", a.getSpotify)
			r.Post("/spotify/authorize", a.authorizeSpotify)
			r.Post("/spotify/test", a.testSpotify)

			r.Get("/settings/email", a.getEmail)
			r.Put("/settings/email", a.putEmail)
			r.Post("/settings/email/test", a.testEmail)

			r.Get("/history", a.history)
			r.Get("/tracks", a.tracks)
			r.Get("/tracks/{id}", a.track)
			r.Post("/tracks/{id}/enrich", a.enrichTrack)
			r.Post("/enrichment/retry-failed", a.retryFailed)
			r.Get("/artists", a.artists)
			r.Get("/playlists", a.playlists)
			r.Post("/playlists/{id}/refresh", a.refreshPlaylist)
			a.mountTopPlaylists(r)
			a.mountBackups(r)

			r.Get("/analytics", a.analytics)
			r.Get("/analytics/pages", a.pageAnalytics)
			r.Get("/audit", a.auditLog)
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "resource not found")
		})
	})
}
