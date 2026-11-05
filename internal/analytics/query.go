package analytics

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Report is the analytics response for the admin dashboard.
type Report struct {
	Range  string `json:"range"`
	Bucket string `json:"bucket"`
	// TimeZone is the IANA zone daily buckets were aligned to.
	TimeZone    string        `json:"time_zone"`
	Totals      Totals        `json:"totals"`
	Timeseries  []Point       `json:"timeseries"`
	Routes      []RouteStat   `json:"routes"`
	StatusCodes []StatusCount `json:"status_codes"`
	TopOrigins  []OriginCount `json:"top_origins"`
}

// Totals summarises the range.
type Totals struct {
	Requests      int64   `json:"requests"`
	Errors        int64   `json:"errors"`
	ErrorRate     float64 `json:"error_rate"`
	P50MS         float64 `json:"p50_ms"`
	P95MS         float64 `json:"p95_ms"`
	P99MS         float64 `json:"p99_ms"`
	UniqueClients int64   `json:"unique_clients"`
}

// Point is one time-series bucket.
type Point struct {
	Bucket   time.Time `json:"bucket"`
	Requests int64     `json:"requests"`
	Errors   int64     `json:"errors"`
	P95MS    float64   `json:"p95_ms"`
}

// RouteStat aggregates one route.
type RouteStat struct {
	Route    string  `json:"route"`
	Method   string  `json:"method"`
	Requests int64   `json:"requests"`
	Errors   int64   `json:"errors"`
	P50MS    float64 `json:"p50_ms"`
	P95MS    float64 `json:"p95_ms"`
	AvgMS    float64 `json:"avg_ms"`
}

// StatusCount counts one status code.
type StatusCount struct {
	Code  int   `json:"code"`
	Count int64 `json:"count"`
}

// OriginCount counts requests from one origin.
type OriginCount struct {
	Origin string `json:"origin"`
	Count  int64  `json:"count"`
}

type rangeSpec struct {
	name   string
	since  time.Time
	daily  bool // buckets are calendar days in loc (else UTC hours)
	bucket string
	days   int
	loc    *time.Location
}

// parseRange computes the window. Hourly ranges use absolute UTC hours.
// Daily ranges use calendar days in loc: midnights come from time.Date and
// AddDate in that zone (never from adding 24h), so 23/25-hour days around
// DST transitions, and zones whose rules changed recently, are bucketed
// correctly using the embedded tz database.
func parseRange(s string, now time.Time, loc *time.Location) (rangeSpec, bool) {
	hour := now.UTC().Truncate(time.Hour)
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	switch s {
	case "", "24h":
		return rangeSpec{name: "24h", since: hour.Add(-23 * time.Hour), bucket: "hour", days: 2, loc: loc}, true
	case "7d":
		return rangeSpec{name: "7d", since: hour.Add(-(7*24 - 1) * time.Hour), bucket: "hour", days: 8, loc: loc}, true
	case "30d":
		return rangeSpec{name: "30d", since: today.AddDate(0, 0, -29), daily: true, bucket: "day", days: 31, loc: loc}, true
	}
	return rangeSpec{}, false
}

// bucketOf maps a timestamp to its series bucket.
func (s rangeSpec) bucketOf(t time.Time) time.Time {
	if !s.daily {
		return t.UTC().Truncate(time.Hour)
	}
	l := t.In(s.loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, s.loc)
}

// next advances one bucket.
func (s rangeSpec) next(t time.Time) time.Time {
	if !s.daily {
		return t.Add(time.Hour)
	}
	return t.AddDate(0, 0, 1)
}

// ErrBadZone is returned for unknown time zones.
var ErrBadZone = fmt.Errorf("tz must be an IANA time zone name, e.g. Asia/Ho_Chi_Minh")

// LoadZone validates an IANA zone name ("" means UTC).
func LoadZone(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	if len(name) > 64 || name == "Local" {
		return nil, ErrBadZone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrBadZone
	}
	return loc, nil
}

// ErrBadRange is returned for unknown ranges.
var ErrBadRange = fmt.Errorf("range must be one of 24h, 7d, 30d")

// Query builds the report for a range ("24h", "7d", "30d"); daily buckets
// are aligned to calendar days in loc (nil means UTC).
func (r *Recorder) Query(ctx context.Context, rangeName string, loc *time.Location) (Report, error) {
	// Include not-yet-flushed data.
	r.Flush(ctx)

	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().UTC()
	spec, ok := parseRange(rangeName, now, loc)
	if !ok {
		return Report{}, ErrBadRange
	}

	agg := newAggregator(spec)
	if err := r.scanStats(ctx, spec.since, agg.add); err != nil {
		return Report{}, err
	}
	rep := agg.report(now)
	rep.Totals.UniqueClients, rep.TopOrigins = r.clientCounters(ctx, now, spec.days)
	return rep, nil
}

// statRow is one api_request_stats row.
type statRow struct {
	bucket        time.Time
	route, method string
	status        int
	count         int64
	durSum        float64
	hist          []int64
}

// scanStats streams the rows since the given time into fn.
func (r *Recorder) scanStats(ctx context.Context, since time.Time, fn func(statRow)) error {
	rows, err := r.pool.Query(ctx, `
		SELECT bucket, route, method, status, count, duration_ms_sum, latency_buckets
		FROM api_request_stats WHERE bucket >= $1`, since)
	if err != nil {
		return fmt.Errorf("analytics query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row statRow
		var status int16
		if err := rows.Scan(&row.bucket, &row.route, &row.method, &status, &row.count, &row.durSum, &row.hist); err != nil {
			return err
		}
		row.status = int(status)
		fn(row)
	}
	return rows.Err()
}

// counter accumulates requests, server errors, duration and a histogram.
type counter struct {
	requests, errors int64
	sumMS            float64
	hist             []int64
}

func newCounter() *counter { return &counter{hist: make([]int64, len(BucketBounds)+1)} }

func (c *counter) add(row statRow) {
	c.requests += row.count
	if row.status >= 500 {
		c.errors += row.count
	}
	c.sumMS += row.durSum
	addHist(c.hist, row.hist)
}

// aggregator folds stat rows into totals, a time series, routes and codes.
// It does no I/O, so it can be unit tested.
type aggregator struct {
	spec   rangeSpec
	total  *counter
	series map[time.Time]*counter
	routes map[[2]string]*counter
	codes  map[int]int64
}

func newAggregator(spec rangeSpec) *aggregator {
	return &aggregator{spec: spec, total: newCounter(), series: map[time.Time]*counter{},
		routes: map[[2]string]*counter{}, codes: map[int]int64{}}
}

func (a *aggregator) add(row statRow) {
	a.total.add(row)
	a.codes[row.status] += row.count

	b := a.spec.bucketOf(row.bucket)
	if a.series[b] == nil {
		a.series[b] = newCounter()
	}
	a.series[b].add(row)

	key := [2]string{row.method, row.route}
	if a.routes[key] == nil {
		a.routes[key] = newCounter()
	}
	a.routes[key].add(row)
}

// report builds the response, without the Redis-backed client counters.
func (a *aggregator) report(now time.Time) Report {
	rep := Report{Range: a.spec.name, Bucket: a.spec.bucket, TimeZone: a.spec.loc.String(),
		Timeseries: a.timeseries(now), Routes: a.routeStats(), StatusCodes: a.statusCodes(), TopOrigins: []OriginCount{}}
	t := a.total
	rep.Totals = Totals{Requests: t.requests, Errors: t.errors,
		P50MS: Percentile(t.hist, 0.50), P95MS: Percentile(t.hist, 0.95), P99MS: Percentile(t.hist, 0.99)}
	if t.requests > 0 {
		rep.Totals.ErrorRate = float64(t.errors) / float64(t.requests)
	}
	return rep
}

// timeseries returns every bucket in the window, oldest first, zero-filled.
func (a *aggregator) timeseries(now time.Time) []Point {
	out := []Point{}
	end := a.spec.bucketOf(now)
	for b := a.spec.since; !b.After(end); b = a.spec.next(b) {
		p := Point{Bucket: b}
		if c := a.series[b]; c != nil {
			p.Requests, p.Errors, p.P95MS = c.requests, c.errors, Percentile(c.hist, 0.95)
		}
		out = append(out, p)
	}
	return out
}

func (a *aggregator) routeStats() []RouteStat {
	out := make([]RouteStat, 0, len(a.routes))
	for key, c := range a.routes {
		rs := RouteStat{Method: key[0], Route: key[1], Requests: c.requests, Errors: c.errors,
			P50MS: Percentile(c.hist, 0.50), P95MS: Percentile(c.hist, 0.95)}
		if c.requests > 0 {
			rs.AvgMS = round2(c.sumMS / float64(c.requests))
		}
		out = append(out, rs)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests == out[j].Requests {
			return out[i].Route < out[j].Route
		}
		return out[i].Requests > out[j].Requests
	})
	return out
}

func (a *aggregator) statusCodes() []StatusCount {
	out := make([]StatusCount, 0, len(a.codes))
	for c, n := range a.codes {
		out = append(out, StatusCount{Code: c, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// clientCounters reads unique clients and top origins from the per-UTC-day
// Redis counters overlapping the window. Errors degrade to zero or empty.
func (r *Recorder) clientCounters(ctx context.Context, now time.Time, days int) (int64, []OriginCount) {
	var uvKeys, originKeys []string
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -i).Format("20060102")
		uvKeys = append(uvKeys, keyPrefix+"uv:"+d)
		originKeys = append(originKeys, keyPrefix+"origins:"+d)
	}
	unique, _ := r.rdb.PFCount(ctx, uvKeys...).Result()

	counts := map[string]int64{}
	for _, k := range originKeys {
		zs, err := r.rdb.ZRevRangeWithScores(ctx, k, 0, 49).Result()
		if err != nil {
			continue
		}
		for _, z := range zs {
			if o, ok := z.Member.(string); ok {
				counts[o] += int64(z.Score)
			}
		}
	}
	return unique, topOrigins(counts, 10)
}

func topOrigins(counts map[string]int64, n int) []OriginCount {
	out := make([]OriginCount, 0, len(counts))
	for o, c := range counts {
		out = append(out, OriginCount{Origin: o, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Origin < out[j].Origin
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func addHist(dst, src []int64) {
	for i := 0; i < len(dst) && i < len(src); i++ {
		dst[i] += src[i]
	}
}

// Percentile estimates the q-quantile (0..1) from histogram counts using
// linear interpolation within the bucket.
func Percentile(hist []int64, q float64) float64 {
	var total int64
	for _, n := range hist {
		total += n
	}
	if total == 0 {
		return 0
	}
	rank := q * float64(total)
	var cum int64
	for i, n := range hist {
		if n == 0 {
			continue
		}
		if float64(cum+n) >= rank {
			lower := 0.0
			if i > 0 {
				lower = BucketBounds[i-1]
			}
			upper := lower * 2
			if i < len(BucketBounds) {
				upper = BucketBounds[i]
			}
			frac := (rank - float64(cum)) / float64(n)
			return round2(lower + frac*(upper-lower))
		}
		cum += n
	}
	return BucketBounds[len(BucketBounds)-1]
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }
