// Package httpapi is the public, read-only REST API (/api/v1) and the root router.
package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/store"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

// Options configures the router.
type Options struct {
	AllowedOrigins     []string
	CacheTTL           time.Duration
	RateLimitPerMinute int
	// TrustedProxyHops / ClientIPHeader define how the client IP is resolved
	// (used for logging and rate limiting). See config.API.
	TrustedProxyHops int
	ClientIPHeader   string
	Ready            http.HandlerFunc
	// V1Middleware wraps the public /api/v1 routes (e.g. analytics recording).
	V1Middleware []func(http.Handler) http.Handler
	// Mount registers extra routes (admin API, SPA) on the root router.
	Mount func(r chi.Router)
	// ShowPrivatePlaylists exposes private playlists on the public API.
	ShowPrivatePlaylists bool
	// TopPlaylists lists the enabled "Top 50" playlist kinds and their zone.
	TopPlaylists topplaylists.Config
	// Events receives public-page analytics beacons (POST /api/v1/events).
	Events http.HandlerFunc
	// Site serves the public frontend at "/" and every non-API path that
	// no other route matched (assets, 404 page).
	Site http.Handler
}

// Handler serves the public API.
type Handler struct {
	store *store.Store
	top   topPlaylistStore
	rdb   *redis.Client
	log   *slog.Logger
	opts  Options
	now   func() time.Time // test seam; nil = time.Now
	// streams caps open now-playing streams; streamAge is a test seam for
	// their lifetime (nil = randomStreamAge).
	streams   *streamSlots
	streamAge func() time.Duration
}

func (h *Handler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// NewRouter builds the HTTP router.
func NewRouter(st *store.Store, rdb *redis.Client, log *slog.Logger, opts Options) http.Handler {
	h := &Handler{store: st, rdb: rdb, log: log, opts: opts, streams: newStreamSlots()}
	if st != nil {
		h.top = st
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	switch {
	case opts.ClientIPHeader != "":
		r.Use(middleware.ClientIPFromHeader(opts.ClientIPHeader))
	case opts.TrustedProxyHops > 0:
		r.Use(middleware.ClientIPFromXFFTrustedProxies(opts.TrustedProxyHops))
	default:
		r.Use(middleware.ClientIPFromRemoteAddr)
	}
	r.Use(recoverer(log))
	r.Use(accessLog(log))
	r.Use(securityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: opts.AllowedOrigins,
		AllowedMethods: []string{http.MethodGet, http.MethodHead, http.MethodOptions},
		AllowedHeaders: []string{"Accept", "Content-Type"},
		MaxAge:         600,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	if opts.Ready != nil {
		r.Get("/readyz", opts.Ready)
	}

	r.Route("/api/v1", func(r chi.Router) {
		for _, m := range opts.V1Middleware {
			r.Use(m)
		}
		if opts.RateLimitPerMinute > 0 {
			r.Use(httprate.LimitBy(opts.RateLimitPerMinute, time.Minute, clientIPKey))
		}
		// The SSE stream is long-lived: no request timeout.
		r.Get("/now-playing/stream", h.nowPlayingStream)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(10 * time.Second))
			r.Get("/now-playing", h.nowPlaying)
			r.Get("/history", h.history)
			r.Get("/tracks/{id}", h.cached(h.trackDetail))
			r.Get("/stats/summary", h.cached(h.summary, "range"))
			r.Get("/stats/top-tracks", h.cached(h.topTracks, "range", "limit"))
			r.Get("/stats/top-artists", h.cached(h.topArtists, "range", "limit"))
			r.Get("/stats/genres", h.cached(h.genres, "range", "limit"))
			r.Get("/stats/audio-profile", h.cached(h.audioProfile, "range"))
			r.Get("/playlists/recent", h.cached(h.recentPlaylists, "limit"))
			r.Get("/playlists/top", h.cached(h.topPlaylists, "limit"))
			r.Get("/timezone", h.timezone)
			if opts.Events != nil {
				r.Post("/events", opts.Events)
			}
		})
	})

	if opts.Mount != nil {
		opts.Mount(r)
	}

	r.Get("/api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	if opts.Site != nil {
		r.Get("/", opts.Site.ServeHTTP)
		r.Head("/", opts.Site.ServeHTTP)
	}

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		p := req.URL.Path
		isAPI := p == "/api" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/admin/api/")
		if opts.Site != nil && !isAPI && (req.Method == http.MethodGet || req.Method == http.MethodHead) {
			opts.Site.ServeHTTP(w, req)
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})
	return r
}

// clientIPKey buckets rate limits by the resolved client IP (IPv6 by /64),
// falling back to the TCP peer when no trusted client IP is available.
func clientIPKey(r *http.Request) (string, error) {
	ip := middleware.GetClientIP(r.Context())
	if ip == "" {
		ip = r.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
	}
	return httprate.CanonicalizeIP(ip), nil
}
