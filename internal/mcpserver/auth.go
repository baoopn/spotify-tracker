package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/singleflight"

	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

const (
	// DefaultSettingsTTL is how long the MCP settings are cached.
	DefaultSettingsTTL = 30 * time.Second
	// touchEvery throttles the token last-used updates.
	touchEvery = 5 * time.Minute
	// touchTimeout bounds one last-used update.
	touchTimeout = 5 * time.Second
	// settingsLoadTimeout bounds one settings load.
	settingsLoadTimeout = 5 * time.Second
	// loadRetry is how long the cache waits after a failed load before it
	// queries Postgres again.
	loadRetry = 5 * time.Second
)

// SettingsSource loads the dashboard-managed MCP settings (implemented by
// *settings.Store).
type SettingsSource interface {
	MCP(ctx context.Context) (settings.MCP, error)
	TouchMCPToken(ctx context.Context, hash string, at time.Time) error
}

// SettingsCache caches the MCP settings so that MCP requests do not hit
// Postgres every time. The admin API calls Invalidate after a change, so
// changes apply at once in this process (and within the TTL elsewhere).
//
// Loads run outside the mutex and are shared by concurrent callers. When a
// load fails the cache waits loadRetry before trying again, so an outage
// does not hammer Postgres; meanwhile it serves the last value if the TTL
// merely expired, but never a value from before an Invalidate (a revoked
// token must not keep working).
type SettingsCache struct {
	src SettingsSource
	ttl time.Duration
	log *slog.Logger
	now func() time.Time

	flight singleflight.Group

	mu        sync.Mutex
	cur       settings.MCP
	loaded    bool                 // cur is fresh until fetchedAt+ttl
	stale     bool                 // cur may be served when a reload fails
	gen       uint64               // bumped by Invalidate
	fetchedAt time.Time            // of cur
	retryAt   time.Time            // no load before this, after a failure
	loadErr   error                // of the last failed load
	touchedAt map[string]time.Time // token hash → last touch sent
	wg        sync.WaitGroup       // pending touches (tests)
}

// NewSettingsCache returns a cache over src (ttl <= 0 uses
// DefaultSettingsTTL).
func NewSettingsCache(src SettingsSource, ttl time.Duration, log *slog.Logger) *SettingsCache {
	if ttl <= 0 {
		ttl = DefaultSettingsTTL
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &SettingsCache{src: src, ttl: ttl, log: log, now: time.Now, touchedAt: map[string]time.Time{}}
}

// Invalidate drops the cached settings; the next request reloads them, and
// if that fails the settings are unavailable (the old value is not used).
func (c *SettingsCache) Invalidate() {
	c.mu.Lock()
	c.loaded, c.stale = false, false
	c.gen++
	c.retryAt = time.Time{}
	c.mu.Unlock()
}

// get returns the settings. When loading fails it returns the last value
// loaded since the last Invalidate, if any, else the error.
func (c *SettingsCache) get(ctx context.Context) (settings.MCP, error) {
	c.mu.Lock()
	now := c.now()
	if c.loaded && now.Sub(c.fetchedAt) < c.ttl {
		v := c.cur
		c.mu.Unlock()
		return v, nil
	}
	if now.Before(c.retryAt) { // backing off after a failed load
		v, stale, err := c.cur, c.stale, c.loadErr
		c.mu.Unlock()
		if stale {
			return v, nil
		}
		return settings.MCP{}, err
	}
	gen := c.gen
	c.mu.Unlock()

	// One load per generation at a time; it must not die with the request
	// that happened to start it.
	_, err, _ := c.flight.Do(strconv.FormatUint(gen, 10), func() (any, error) {
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settingsLoadTimeout)
		defer cancel()
		v, err := c.src.MCP(lctx)
		if errors.Is(err, settings.ErrNotFound) {
			v, err = settings.DefaultMCP, nil
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.gen != gen { // invalidated meanwhile: the result may be outdated
			return nil, err
		}
		if err != nil {
			c.retryAt, c.loadErr = c.now().Add(loadRetry), err
			return nil, err
		}
		c.cur, c.loaded, c.stale, c.fetchedAt = v, true, true, c.now()
		c.retryAt, c.loadErr = time.Time{}, nil
		return nil, nil
	})

	c.mu.Lock()
	v, stale, cgen := c.cur, c.stale, c.gen
	c.mu.Unlock()
	if err != nil {
		if stale && cgen == gen {
			c.log.WarnContext(ctx, "mcp settings unavailable; using the last loaded value", logging.Err(err))
			return v, nil
		}
		return settings.MCP{}, err
	}
	if !stale || cgen != gen {
		// Loaded fine, but an Invalidate raced in after the load: the new
		// generation has not been loaded yet. Try once more.
		return c.get(ctx)
	}
	return v, nil
}

// touch records a use of the stored token, at most once per touchEvery and
// in the background: it never delays or fails the MCP call.
func (c *SettingsCache) touch(st settings.MCP) {
	now := c.now()
	c.mu.Lock()
	last := c.touchedAt[st.TokenHash]
	if st.TokenLastUsedAt != nil && st.TokenLastUsedAt.After(last) {
		last = *st.TokenLastUsedAt
	}
	if now.Sub(last) < touchEvery {
		c.mu.Unlock()
		return
	}
	clear(c.touchedAt) // only the current token matters
	c.touchedAt[st.TokenHash] = now
	c.mu.Unlock()

	c.wg.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), touchTimeout)
		defer cancel()
		if err := c.src.TouchMCPToken(ctx, st.TokenHash, now); err != nil {
			c.log.WarnContext(ctx, "mcp token last-used update failed", logging.Err(err))
		}
	})
}

// authorizer picks the MCP server for a request from its bearer token.
type authorizer struct {
	envToken []byte         // MCP_ADMIN_TOKEN; empty = none
	settings *SettingsCache // nil = env token only, admin tools on
	admin    *mcp.Server    // nil when the admin tools are not available
	log      *slog.Logger
}

func (a *authorizer) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearer(r.Header.Get("Authorization"))
		if !ok {
			a.reject(w, r, http.StatusUnauthorized, "unauthorized", "unsupported authorization scheme")
			return
		}
		if tok == "" { // anonymous: public tools only
			next.ServeHTTP(w, r)
			return
		}
		st, err := a.state(r.Context())
		envMatch := len(a.envToken) > 0 && subtle.ConstantTimeCompare([]byte(tok), a.envToken) == 1
		switch {
		case envMatch:
			if err != nil {
				st = settings.DefaultMCP
			}
		case err != nil:
			a.log.ErrorContext(r.Context(), "mcp settings unavailable", logging.Err(err))
			a.reject(w, r, http.StatusServiceUnavailable, "unavailable", "authentication is temporarily unavailable")
			return
		case st.TokenHash != "" &&
			subtle.ConstantTimeCompare([]byte(settings.HashMCPToken(tok)), []byte(st.TokenHash)) == 1:
			a.settings.touch(st)
		default:
			a.reject(w, r, http.StatusUnauthorized, "unauthorized", "invalid bearer token")
			return
		}
		if st.AdminToolsEnabled && a.admin != nil {
			r = r.WithContext(context.WithValue(r.Context(), serverKey{}, a.admin))
		}
		next.ServeHTTP(w, r)
	})
}

func (a *authorizer) state(ctx context.Context) (settings.MCP, error) {
	if a.settings == nil {
		return settings.DefaultMCP, nil
	}
	return a.settings.get(ctx)
}

// bearer extracts the token of an Authorization header. An empty header or
// "Bearer" with no token is anonymous (""; true); a scheme other than
// Bearer reports false.
func bearer(h string) (string, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return "", true
	}
	scheme, tok, _ := strings.Cut(h, " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(tok), true
}

func (a *authorizer) reject(w http.ResponseWriter, r *http.Request, status int, code, reason string) {
	if status == http.StatusUnauthorized {
		ip := middleware.GetClientIP(r.Context())
		if ip == "" {
			ip = r.RemoteAddr
		}
		a.log.WarnContext(r.Context(), "mcp authentication failed",
			slog.String("reason", reason), slog.String("client_ip", ip))
		w.Header().Set("WWW-Authenticate", `Bearer realm="spotify-tracker"`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]map[string]string{
		"error": {"code": code, "message": reason},
	})
}
