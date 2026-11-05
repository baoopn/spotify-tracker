// Package analytics records public API traffic (per-route counts, status
// codes, latency histograms, unique clients, origins) and visits to the
// public page (views, unique visitors, time on page, referrers, devices,
// browsers, countries), and answers dashboard queries over both.
//
// Requests are aggregated in memory per hour/route/method/status and flushed
// to Postgres every few seconds; unique clients (salted IP hashes) and
// origins are counted in Redis per day.
package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// BucketBounds are the latency histogram upper bounds in milliseconds; a
// final +Inf bucket follows.
var BucketBounds = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

const (
	keyPrefix = "spotify-tracker:analytics:"
	dayTTL    = 40 * 24 * time.Hour
)

type aggKey struct {
	bucket time.Time
	route  string
	method string
	status int
}

type aggVal struct {
	count   int64
	sumMS   float64
	maxMS   float64
	buckets []int64
}

// Recorder collects request metrics.
type Recorder struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
	salt string
	log  *slog.Logger

	mu    sync.Mutex
	agg   map[aggKey]*aggVal
	pages map[pageKey]*pageVal

	// clients and visitors are drained by Run; a full queue drops the event
	// (analytics are best effort).
	clients  chan clientEvent
	visitors chan visitorEvent
}

type clientEvent struct{ day, client, origin string }

// NewRecorder creates a Recorder. salt keeps client hashes unlinkable to IPs.
func NewRecorder(pool *pgxpool.Pool, rdb *redis.Client, salt string, log *slog.Logger) *Recorder {
	return &Recorder{pool: pool, rdb: rdb, salt: salt, log: log, agg: map[aggKey]*aggVal{},
		pages: map[pageKey]*pageVal{}, clients: make(chan clientEvent, 4096),
		visitors: make(chan visitorEvent, 4096)}
}

// Middleware records every request that reaches it. Mount it on the public
// /api/v1 routes only (admin, health and metrics are excluded by design).
func (r *Recorder) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, req.ProtoMajor)
		next.ServeHTTP(ww, req)

		route := chi.RouteContext(req.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		ms := float64(time.Since(start).Microseconds()) / 1000
		r.observe(start.UTC().Truncate(time.Hour), route, req.Method, status, ms)

		client := middleware.GetClientIP(req.Context())
		if client == "" {
			client = req.RemoteAddr
		}
		origin := originOf(req)
		day := start.UTC().Format("20060102")
		select {
		case r.clients <- clientEvent{day: day, client: client, origin: origin}:
		default:
		}
	})
}

func (r *Recorder) observe(bucket time.Time, route, method string, status int, ms float64) {
	k := aggKey{bucket: bucket, route: route, method: method, status: status}
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.agg[k]
	if !ok {
		v = &aggVal{buckets: make([]int64, len(BucketBounds)+1)}
		r.agg[k] = v
	}
	v.count++
	v.sumMS += ms
	if ms > v.maxMS {
		v.maxMS = ms
	}
	v.buckets[bucketIndex(ms)]++
}

func bucketIndex(ms float64) int {
	for i, b := range BucketBounds {
		if ms <= b {
			return i
		}
	}
	return len(BucketBounds)
}

func (r *Recorder) trackClient(ctx context.Context, day, client, origin string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	sum := sha256.Sum256([]byte(r.salt + "|" + day + "|" + client))
	pipe := r.rdb.Pipeline()
	pipe.PFAdd(ctx, keyPrefix+"uv:"+day, hex.EncodeToString(sum[:12]))
	pipe.Expire(ctx, keyPrefix+"uv:"+day, dayTTL)
	if origin != "" {
		pipe.ZIncrBy(ctx, keyPrefix+"origins:"+day, 1, origin)
		pipe.Expire(ctx, keyPrefix+"origins:"+day, dayTTL)
	}
	_, _ = pipe.Exec(ctx)
}

func originOf(req *http.Request) string {
	if o := req.Header.Get("Origin"); o != "" && o != "null" {
		return o
	}
	if ref := req.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return ""
}

// Run flushes aggregates every interval until ctx is cancelled (with a final
// flush on shutdown).
func (r *Recorder) Run(ctx context.Context, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			r.Flush(fctx)
			cancel()
			return nil
		case <-t.C:
			r.Flush(ctx)
		case ev := <-r.clients:
			r.trackClient(ctx, ev.day, ev.client, ev.origin)
		case ev := <-r.visitors:
			r.trackVisitor(ctx, ev)
		}
	}
}

// Flush writes in-memory aggregates to Postgres.
func (r *Recorder) Flush(ctx context.Context) {
	if err := r.flushPages(ctx); err != nil {
		r.log.WarnContext(ctx, "flush page analytics failed; re-queueing", slog.Any("error", err))
	}
	r.mu.Lock()
	pending := r.agg
	r.agg = map[aggKey]*aggVal{}
	r.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	batch := &pgx.Batch{}
	for k, v := range pending {
		batch.Queue(`
			INSERT INTO api_request_stats AS s
				(bucket, route, method, status, count, duration_ms_sum, duration_ms_max, latency_buckets)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (bucket, route, method, status) DO UPDATE SET
				count = s.count + EXCLUDED.count,
				duration_ms_sum = s.duration_ms_sum + EXCLUDED.duration_ms_sum,
				duration_ms_max = GREATEST(s.duration_ms_max, EXCLUDED.duration_ms_max),
				latency_buckets = (
					SELECT array_agg(a + b ORDER BY i)
					FROM unnest(s.latency_buckets, EXCLUDED.latency_buckets) WITH ORDINALITY AS t(a, b, i)
				)`,
			k.bucket, k.route, k.method, k.status, v.count, v.sumMS, v.maxMS, v.buckets)
	}
	if err := r.pool.SendBatch(ctx, batch).Close(); err != nil {
		r.log.WarnContext(ctx, "flush analytics failed; re-queueing", slog.Any("error", err))
		r.mu.Lock()
		for k, v := range pending {
			if cur, ok := r.agg[k]; ok {
				cur.count += v.count
				cur.sumMS += v.sumMS
				cur.maxMS = max(cur.maxMS, v.maxMS)
				for i := range cur.buckets {
					cur.buckets[i] += v.buckets[i]
				}
			} else {
				r.agg[k] = v
			}
		}
		r.mu.Unlock()
	}
}

// Prune deletes aggregates older than retention.
func (r *Recorder) Prune(ctx context.Context, retention time.Duration) error {
	cutoff := time.Now().Add(-retention)
	if _, err := r.pool.Exec(ctx, `DELETE FROM api_request_stats WHERE bucket < $1`, cutoff); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM page_view_stats WHERE bucket < $1`, cutoff)
	return err
}
