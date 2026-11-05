package analytics

import (
	"testing"
	"time"
)

func TestBucketIndex(t *testing.T) {
	cases := map[float64]int{0.3: 0, 5: 0, 5.1: 1, 99: 4, 100: 4, 4999: 9, 9000: 10}
	for ms, want := range cases {
		if got := bucketIndex(ms); got != want {
			t.Errorf("bucketIndex(%v) = %d, want %d", ms, got, want)
		}
	}
}

func TestPercentile(t *testing.T) {
	h := make([]int64, len(BucketBounds)+1)
	if Percentile(h, 0.95) != 0 {
		t.Fatal("empty histogram should be 0")
	}
	h[0] = 90 // <=5ms
	h[4] = 10 // 50..100ms
	if p50 := Percentile(h, 0.5); p50 > 5 {
		t.Errorf("p50 = %v, want <= 5", p50)
	}
	if p95 := Percentile(h, 0.95); p95 < 50 || p95 > 100 {
		t.Errorf("p95 = %v, want within 50..100", p95)
	}
}

func TestParseRange(t *testing.T) {
	now := time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC)
	r, ok := parseRange("24h", now, time.UTC)
	if !ok || r.bucket != "hour" || !r.since.Equal(time.Date(2026, 11, 4, 19, 0, 0, 0, time.UTC)) {
		t.Errorf("24h = %+v", r)
	}
	r, ok = parseRange("30d", now, time.UTC)
	if !ok || r.bucket != "day" || !r.since.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("30d = %+v", r)
	}
	if _, ok := parseRange("1y", now, time.UTC); ok {
		t.Error("expected unknown range")
	}
}

// Daily buckets must follow local calendar days across a DST change:
// in America/New_York, 2026-11-01 is a 25-hour day (clocks fall back).
func TestDailyBucketsAcrossDST(t *testing.T) {
	ny, err := LoadZone("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	spec, _ := parseRange("30d", now, ny)
	var days []time.Time
	for b := spec.since; !b.After(spec.bucketOf(now)); b = spec.next(b) {
		days = append(days, b)
	}
	if len(days) != 30 {
		t.Fatalf("want 30 daily buckets, got %d", len(days))
	}
	for _, d := range days {
		if d.Hour() != 0 || d.Minute() != 0 {
			t.Errorf("bucket %v is not local midnight", d)
		}
	}
	// 01:30 EST on the repeated hour belongs to Nov 1 local.
	repeated := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	if got := spec.bucketOf(repeated); got.Day() != 1 || got.Month() != time.November {
		t.Errorf("bucketOf(%v) = %v", repeated, got)
	}
	// Half-hour offset zone: 18:45 UTC is 00:15 IST the next day.
	kol, _ := LoadZone("Asia/Kolkata")
	spec, _ = parseRange("30d", now, kol)
	if b := spec.bucketOf(time.Date(2026, 11, 2, 18, 45, 0, 0, time.UTC)); b.Day() != 3 {
		t.Errorf("Kolkata bucket = %v", b)
	}
	if _, err := LoadZone("Mars/Olympus"); err == nil {
		t.Error("expected bad zone")
	}
}

func TestAggregatorReport(t *testing.T) {
	now := time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC)
	spec, _ := parseRange("24h", now, time.UTC)
	agg := newAggregator(spec)
	h := func(i int, n int64) []int64 {
		b := make([]int64, len(BucketBounds)+1)
		b[i] = n
		return b
	}
	cur := now.Truncate(time.Hour)
	agg.add(statRow{bucket: cur, route: "/api/v1/now-playing", method: "GET", status: 200, count: 90, durSum: 270, hist: h(0, 90)})
	agg.add(statRow{bucket: cur, route: "/api/v1/now-playing", method: "GET", status: 503, count: 10, durSum: 900, hist: h(4, 10)})
	agg.add(statRow{bucket: cur.Add(-3 * time.Hour), route: "/api/v1/history", method: "GET", status: 200, count: 5, durSum: 50, hist: h(1, 5)})

	rep := agg.report(now)
	if rep.Totals.Requests != 105 || rep.Totals.Errors != 10 {
		t.Fatalf("totals = %+v", rep.Totals)
	}
	if len(rep.Timeseries) != 24 {
		t.Fatalf("want 24 hourly points, got %d", len(rep.Timeseries))
	}
	last := rep.Timeseries[len(rep.Timeseries)-1]
	if last.Requests != 100 || last.Errors != 10 {
		t.Errorf("last bucket = %+v", last)
	}
	if rep.Routes[0].Route != "/api/v1/now-playing" || rep.Routes[0].Requests != 100 || rep.Routes[0].AvgMS != 11.7 {
		t.Errorf("routes = %+v", rep.Routes)
	}
	if len(rep.StatusCodes) != 2 || rep.StatusCodes[0].Code != 200 || rep.StatusCodes[0].Count != 95 {
		t.Errorf("codes = %+v", rep.StatusCodes)
	}
	got := topOrigins(map[string]int64{"b": 2, "a": 2, "c": 5}, 2)
	if len(got) != 2 || got[0].Origin != "c" || got[1].Origin != "a" {
		t.Errorf("topOrigins = %+v", got)
	}
}
