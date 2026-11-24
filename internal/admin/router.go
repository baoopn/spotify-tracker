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
			r.Get("/tracks/{id}/lyrics", a.trackLyrics)
			r.Get("/lyrics/search", a.searchLyrics)
			r.Get("/lyrics/translation-queue", a.translationQueue)
			r.Post("/tracks/{id}/translation/requeue", a.requeueTranslation)
			r.Post("/tracks/{id}/enrich", a.enrichTrack)
			r.Post("/enrichment/retry-failed", a.retryFailed)
			r.Get("/artists", a.artists)
			r.Get("/playlists", a.playlists)
			r.Post("/playlists/{id}/refresh", a.refreshPlaylist)
			a.mountTopPlaylists(r)
			a.mountBackups(r)
			a.mountMCP(r)

			r.Get("/analytics", a.analytics)
			r.Get("/analytics/pages", a.pageAnalytics)
			r.Get("/audit", a.auditLog)
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "resource not found")
		})
	})
}

// mcpUser is the actor injected by ReadOnlyHandler.
var mcpUser = User{ID: "mcp", Email: "mcp", Name: "MCP"}

// ReadOnlyHandler serves a read-only subset of the admin GET routes, at the
// same paths relative to /admin/api (e.g. /overview), with no Clerk
// authentication, rate limit or timeout. It is for in-process use by the MCP
// server only, which authenticates callers itself: never mount it on a
// public router. Lyrics (track lyrics and lyrics search) are served here, so
// they reach admin-token MCP callers only. The one write route is the
// translation re-queue (POST /tracks/{id}/translation/requeue), which only
// reorders the translation queue. Other write routes, /settings/email (SMTP
// settings), /auth/me, /audit, /config and the /mcp settings (token minting)
// are left out on purpose.
func ReadOnlyHandler(a *API) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, mcpUser)))
		})
	})
	r.Get("/overview", a.overview)
	r.Get("/spotify", a.getSpotify)
	r.Get("/history", a.history)
	r.Get("/tracks", a.tracks)
	r.Get("/tracks/{id}", a.track)
	r.Get("/tracks/{id}/lyrics", a.trackLyrics)
	r.Get("/lyrics/search", a.searchLyrics)
	r.Get("/lyrics/translation-queue", a.translationQueue)
	r.Post("/tracks/{id}/translation/requeue", a.requeueTranslation)
	r.Get("/artists", a.artists)
	r.Get("/playlists", a.playlists)
	r.Get("/top-playlists", a.listTopPlaylists)
	r.Get("/top-playlists/{period}/{start}", a.topPlaylist)
	r.Get("/backups", a.getBackups)
	r.Get("/analytics", a.analytics)
	r.Get("/analytics/pages", a.pageAnalytics)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})
	return r
}
