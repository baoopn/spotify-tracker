package analytics

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// PageReport is the public-page analytics response for the admin dashboard.
type PageReport struct {
	Range      string      `json:"range"`
	Bucket     string      `json:"bucket"`
	TimeZone   string      `json:"time_zone"`
	Totals     PageTotals  `json:"totals"`
	Timeseries []PagePoint `json:"timeseries"`
	Paths      []DimCount  `json:"paths"`
	Referrers  []DimCount  `json:"referrers"`
	Devices    []DimCount  `json:"devices"`
	Browsers   []DimCount  `json:"browsers"`
	Countries  []DimCount  `json:"countries"`
}

// PageTotals summarises the range. Visitors are unique per UTC day and
// de-duplicated across the days the range overlaps; LiveNow counts visitors
// seen in the last five minutes.
type PageTotals struct {
	Views        int64 `json:"views"`
	Visitors     int64 `json:"visitors"`
	AvgVisibleMS int64 `json:"avg_visible_ms"`
	LiveNow      int64 `json:"live_now"`
}

// PagePoint is one time-series bucket.
type PagePoint struct {
	Bucket time.Time `json:"bucket"`
	Views  int64     `json:"views"`
}

// DimCount is the view count for one value of a dimension. An empty Name
// means direct visits (referrers) or unknown (countries).
type DimCount struct {
	Name  string `json:"name"`
	Views int64  `json:"views"`
}

type pageRow struct {
	bucket                                   time.Time
	path, device, browser, country, referrer string
	views, visibleMS                         int64
}

// pageAggregator folds rows into a report; it does no I/O.
type pageAggregator struct {
	spec                      rangeSpec
	views, visibleMS          int64
	series                    map[time.Time]int64
	paths, referrers, devices map[string]int64
	browsers, countries       map[string]int64
}

func newPageAggregator(spec rangeSpec) *pageAggregator {
	return &pageAggregator{spec: spec, series: map[time.Time]int64{}, paths: map[string]int64{},
		referrers: map[string]int64{}, devices: map[string]int64{}, browsers: map[string]int64{},
		countries: map[string]int64{}}
}

func (a *pageAggregator) add(row pageRow) {
	a.views += row.views
	a.visibleMS += row.visibleMS
	if row.views == 0 {
		return // time-on-page only
	}
	a.series[a.spec.bucketOf(row.bucket)] += row.views
	a.paths[row.path] += row.views
	a.referrers[row.referrer] += row.views
	a.devices[row.device] += row.views
	a.browsers[row.browser] += row.views
	a.countries[row.country] += row.views
}

func (a *pageAggregator) report(now time.Time) PageReport {
	rep := PageReport{Range: a.spec.name, Bucket: a.spec.bucket, TimeZone: a.spec.loc.String(),
		Paths: topDims(a.paths, 10), Referrers: topDims(a.referrers, 10), Devices: topDims(a.devices, 10),
		Browsers: topDims(a.browsers, 10), Countries: topDims(a.countries, 15)}
	rep.Totals.Views = a.views
	if a.views > 0 {
		rep.Totals.AvgVisibleMS = a.visibleMS / a.views
	}
	end := a.spec.bucketOf(now)
	for b := a.spec.since; !b.After(end); b = a.spec.next(b) {
		rep.Timeseries = append(rep.Timeseries, PagePoint{Bucket: b, Views: a.series[b]})
	}
	if rep.Timeseries == nil {
		rep.Timeseries = []PagePoint{}
	}
	return rep
}

func topDims(m map[string]int64, n int) []DimCount {
	out := make([]DimCount, 0, len(m))
	for k, v := range m {
		if v > 0 {
			out = append(out, DimCount{Name: k, Views: v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Views == out[j].Views {
			return out[i].Name < out[j].Name
		}
		return out[i].Views > out[j].Views
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// QueryPages builds the public-page report for a range ("24h", "7d", "30d").
func (r *Recorder) QueryPages(ctx context.Context, rangeName string, loc *time.Location) (PageReport, error) {
	r.Flush(ctx) // include not-yet-flushed data
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().UTC()
	spec, ok := parseRange(rangeName, now, loc)
	if !ok {
		return PageReport{}, ErrBadRange
	}
	agg := newPageAggregator(spec)
	rows, err := r.pool.Query(ctx, `
		SELECT bucket, path, device, browser, country, referrer, views, visible_ms
		FROM page_view_stats WHERE bucket >= $1`, spec.since)
	if err != nil {
		return PageReport{}, fmt.Errorf("page analytics query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row pageRow
		if err := rows.Scan(&row.bucket, &row.path, &row.device, &row.browser, &row.country,
			&row.referrer, &row.views, &row.visibleMS); err != nil {
			return PageReport{}, err
		}
		agg.add(row)
	}
	if err := rows.Err(); err != nil {
		return PageReport{}, err
	}
	rep := agg.report(now)

	keys := make([]string, 0, spec.days)
	for i := 0; i < spec.days; i++ {
		keys = append(keys, keyPageUV+now.AddDate(0, 0, -i).Format("20060102"))
	}
	rep.Totals.Visitors, _ = r.rdb.PFCount(ctx, keys...).Result()
	cutoff := strconv.FormatInt(now.Add(-livePeriod).Unix(), 10)
	rep.Totals.LiveNow, _ = r.rdb.ZCount(ctx, keyPageLive, cutoff, "+inf").Result()
	return rep, nil
}
