package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

// Public page analytics: the public site sends a small beacon on each page
// view and when the page is hidden (time on page). Nothing identifies a
// person: no cookies or storage, IPs are never stored, and unique visitors
// come from a salted hash of IP + user agent that changes every UTC day.

const (
	maxEventBytes = 2 << 10
	maxVisibleMS  = 30 * 60 * 1000 // cap one report at 30 minutes
	livePeriod    = 5 * time.Minute
	keyPageUV     = keyPrefix + "pages:uv:"
	keyPageLive   = keyPrefix + "pages:live"
)

type pageKey struct {
	bucket                                   time.Time
	path, device, browser, country, referrer string
}

type pageVal struct{ views, visibleMS int64 }

// pageEvent is the beacon body (POST /api/v1/events).
type pageEvent struct {
	Type      string `json:"type"` // "pageview" | "engagement"
	Path      string `json:"path"`
	Referrer  string `json:"referrer"`
	Width     int    `json:"width"`
	VisibleMS int64  `json:"visible_ms"`
}

type visitorEvent struct{ day, visitor string }

var botUA = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|headless|lighthouse|pingdom|uptime|monitor|preview|curl|wget|python|go-http|java/|node-fetch|axios|playwright|phantom`)

// EventsHandler accepts page-view beacons from the public site. It answers
// 202 for every well-formed event and never reveals whether it was counted
// (bots are dropped silently).
func (r *Recorder) EventsHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	body, err := io.ReadAll(io.LimitReader(req.Body, maxEventBytes+1))
	if err != nil || len(body) > maxEventBytes {
		http.Error(w, "event too large", http.StatusRequestEntityTooLarge)
		return
	}
	var ev pageEvent
	if err := json.Unmarshal(body, &ev); err != nil || (ev.Type != "pageview" && ev.Type != "engagement") {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)

	ua := req.UserAgent()
	if ua == "" || botUA.MatchString(ua) {
		return
	}
	now := time.Now().UTC()
	k := pageKey{
		bucket:  now.Truncate(time.Hour),
		path:    cleanPath(ev.Path),
		device:  deviceOf(ev.Width, ua),
		browser: browserOf(ua),
		country: countryOf(req),
	}
	if ev.Type == "engagement" {
		// Time on page is kept under the view's dimensions without the
		// referrer, which only describes how the visit started.
		if ms := min(max(ev.VisibleMS, 0), maxVisibleMS); ms > 0 {
			r.observePage(k, 0, ms)
		}
		return
	}
	k.referrer = referrerHost(ev.Referrer, req.Host)
	r.observePage(k, 1, 0)

	client := middleware.GetClientIP(req.Context())
	if client == "" {
		client = req.RemoteAddr
	}
	day := now.Format("20060102")
	sum := sha256.Sum256([]byte(r.salt + "|page|" + day + "|" + client + "|" + ua))
	select {
	case r.visitors <- visitorEvent{day: day, visitor: hex.EncodeToString(sum[:12])}:
	default:
	}
}

func (r *Recorder) observePage(k pageKey, views, visibleMS int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.pages[k]
	if v == nil {
		v = &pageVal{}
		r.pages[k] = v
	}
	v.views += views
	v.visibleMS += visibleMS
}

// trackVisitor counts a unique visitor for the day and marks them live.
func (r *Recorder) trackVisitor(ctx context.Context, ev visitorEvent) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	now := time.Now().Unix()
	pipe := r.rdb.Pipeline()
	pipe.PFAdd(ctx, keyPageUV+ev.day, ev.visitor)
	pipe.Expire(ctx, keyPageUV+ev.day, dayTTL)
	pipe.ZAdd(ctx, keyPageLive, redis.Z{Score: float64(now), Member: ev.visitor})
	pipe.ZRemRangeByScore(ctx, keyPageLive, "-inf", strconv.FormatInt(now-int64(livePeriod.Seconds()), 10))
	pipe.Expire(ctx, keyPageLive, 2*livePeriod)
	_, _ = pipe.Exec(ctx)
}

// flushPages writes page aggregates to Postgres, re-queueing them on error.
func (r *Recorder) flushPages(ctx context.Context) error {
	r.mu.Lock()
	pending := r.pages
	r.pages = map[pageKey]*pageVal{}
	r.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for k, v := range pending {
		batch.Queue(`
			INSERT INTO page_view_stats AS s (bucket, path, device, browser, country, referrer, views, visible_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (bucket, path, device, browser, country, referrer) DO UPDATE SET
				views = s.views + EXCLUDED.views, visible_ms = s.visible_ms + EXCLUDED.visible_ms`,
			k.bucket, k.path, k.device, k.browser, k.country, k.referrer, v.views, v.visibleMS)
	}
	err := r.pool.SendBatch(ctx, batch).Close()
	if err != nil {
		r.mu.Lock()
		for k, v := range pending {
			if cur := r.pages[k]; cur != nil {
				cur.views += v.views
				cur.visibleMS += v.visibleMS
			} else {
				r.pages[k] = v
			}
		}
		r.mu.Unlock()
	}
	return err
}

// cleanPath keeps only a short, lower-case pathname.
func cleanPath(p string) string {
	if u, err := url.Parse(p); err == nil {
		p = u.Path
	}
	if p == "" || p[0] != '/' {
		p = "/"
	}
	if len(p) > 100 {
		p = p[:100]
	}
	return strings.ToLower(p)
}

func deviceOf(width int, ua string) string {
	switch {
	case width > 0 && width < 768:
		return "mobile"
	case width > 0 && width < 1024:
		return "tablet"
	case width > 0:
		return "desktop"
	case strings.Contains(ua, "Mobi"):
		return "mobile"
	default:
		return "desktop"
	}
}

func browserOf(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "EdgA/"), strings.Contains(ua, "EdgiOS/"):
		return "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		return "Opera"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		return "Firefox"
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "CriOS/"), strings.Contains(ua, "Chromium/"):
		return "Chrome"
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		return "Safari"
	default:
		return "Other"
	}
}

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// countryOf uses the country Cloudflare attaches to proxied requests.
func countryOf(req *http.Request) string {
	c := strings.ToUpper(strings.TrimSpace(req.Header.Get("CF-IPCountry")))
	if !countryCode.MatchString(c) || c == "XX" || c == "T1" {
		return ""
	}
	return c
}

// referrerHost reduces a referrer to its host. Visits from the site itself
// and unparsable values count as direct.
func referrerHost(ref, selfHost string) string {
	u, err := url.Parse(ref)
	if ref == "" || err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	self := strings.TrimPrefix(strings.ToLower(hostOnly(selfHost)), "www.")
	if host == self || len(host) > 100 {
		return ""
	}
	return host
}

func hostOnly(h string) string {
	if u, err := url.Parse("//" + h); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return h
}
