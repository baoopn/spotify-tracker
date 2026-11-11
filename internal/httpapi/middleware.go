package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/baoopn/spotify-tracker/internal/observability"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
)

func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			code := ww.Status()
			if code == 0 {
				code = http.StatusOK
			}
			clientIP := middleware.GetClientIP(r.Context())
			if clientIP == "" {
				clientIP = r.RemoteAddr
			}
			observability.HTTPRequests.WithLabelValues(route, r.Method, strconv.Itoa(code)).Inc()
			observability.HTTPDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())

			level := slog.LevelInfo
			switch {
			case code >= 500:
				level = slog.LevelError
			case route == "/healthz" || route == "/readyz":
				level = slog.LevelDebug
			}
			log.Log(r.Context(), level, "http request",
				slog.String("method", r.Method), slog.String("path", r.URL.Path),
				slog.String("route", route), slog.Int("status", code),
				slog.Int("bytes", ww.BytesWritten()), slog.Duration("duration", time.Since(start)),
				slog.String("client_ip", clientIP), slog.String("request_id", middleware.GetReqID(r.Context())))
		})
	}
}

func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(p)
					}
					log.ErrorContext(r.Context(), "http panic", slog.Any("panic", p), slog.String("path", r.URL.Path))
					writeError(w, http.StatusInternalServerError, "internal", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// clientCacheControl is sent with cacheable API responses.
const clientCacheControl = "no-cache"

// headerNoStore lets a handler opt a response out of the Redis cache (e.g.
// data that is about to change, like a track still being enriched).
const headerNoStore = "X-Internal-No-Store"

// bufferedWriter captures a handler's response so it can be cached.
type bufferedWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (b *bufferedWriter) Header() http.Header         { return b.header }
func (b *bufferedWriter) Write(p []byte) (int, error) { return b.buf.Write(p) }
func (b *bufferedWriter) WriteHeader(code int)        { b.status = code }

// cacheKey is the Redis key of a cached response: the cache generation, the
// path and only the query params the handler reads (first value each, as
// Query().Get sees them; empty ones mean the default). Unknown params, order
// and duplicates cannot create new entries.
func cacheKey(gen, path string, q url.Values, params []string) string {
	var b strings.Builder
	b.WriteString(gen + "|" + path + "?")
	for _, name := range slices.Sorted(slices.Values(params)) {
		if v := q.Get(name); v != "" {
			b.WriteString(url.QueryEscape(name) + "=" + url.QueryEscape(v) + "&")
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return redisx.KeyCachePrefix + hex.EncodeToString(sum[:16])
}

// cached serves successful JSON responses from Redis for opts.CacheTTL.
// params are the query parameters next reads; only they are part of the
// cache key.
func (h *Handler) cached(next http.HandlerFunc, params ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.opts.CacheTTL <= 0 {
			next(w, r)
			return
		}
		// The generation changes whenever listening data changes (see
		// redisx.BumpCache), so a cached response is never older than the data.
		gen, _ := h.rdb.Get(r.Context(), redisx.KeyCacheGeneration).Result()
		key := cacheKey(gen, r.URL.Path, r.URL.Query(), params)

		// Clients must revalidate every time: freshness is handled server-side,
		// so browser and CDN caches can never serve outdated stats.
		if body, err := h.rdb.Get(r.Context(), key).Bytes(); err == nil && json.Valid(body) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", clientCacheControl)
			w.Header().Set("X-Cache", "HIT")
			_, _ = w.Write(body) //nolint:gosec // cached JSON produced by our own handlers
			return
		}

		bw := &bufferedWriter{header: http.Header{}, status: http.StatusOK}
		next(bw, r)
		for k, v := range bw.header {
			w.Header()[k] = v
		}
		skip := bw.header.Get(headerNoStore) != ""
		w.Header().Del(headerNoStore)
		switch {
		case bw.status == http.StatusOK && !skip:
			_ = h.rdb.Set(r.Context(), key, bw.buf.Bytes(), h.opts.CacheTTL).Err()
			w.Header().Set("Cache-Control", clientCacheControl)
		case skip:
			w.Header().Set("Cache-Control", "no-store")
		}
		w.Header().Set("X-Cache", "MISS")
		w.WriteHeader(bw.status)
		_, _ = w.Write(bw.buf.Bytes())
	}
}
