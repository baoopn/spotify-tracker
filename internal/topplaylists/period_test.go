package topplaylists

import (
	"testing"
	"time"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestLastCompletedUTC(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 4, 5, 0, time.UTC) // Wednesday
	cases := []struct {
		p          Period
		start, end string
	}{
		{Week, "2026-11-02T00:00:00Z", "2026-11-09T00:00:00Z"},
		{Month, "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"},
		{Year, "2025-01-01T00:00:00Z", "2026-01-01T00:00:00Z"},
	}
	for _, c := range cases {
		s, e := LastCompleted(c.p, now, time.UTC)
		if s.Format(time.RFC3339) != c.start || e.Format(time.RFC3339) != c.end {
			t.Errorf("%s: got [%s, %s), want [%s, %s)", c.p, s.Format(time.RFC3339), e.Format(time.RFC3339), c.start, c.end)
		}
	}
}

func TestLastCompletedOnBoundary(t *testing.T) {
	// Exactly Monday 00:00: the week that just ended is the previous one.
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	s, e := LastCompleted(Week, now, time.UTC)
	if !s.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)) || !e.Equal(now) {
		t.Errorf("got [%s, %s)", s, e)
	}
	// One nanosecond before: still the week before that.
	s, _ = LastCompleted(Week, now.Add(-time.Nanosecond), time.UTC)
	if !s.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("got start %s", s)
	}
	// Sunday maps to the week that started six days earlier.
	cs, ce := Bounds(Week, time.Date(2026, 11, 8, 23, 59, 0, 0, time.UTC), time.UTC)
	if cs.Day() != 2 || ce.Day() != 9 {
		t.Errorf("sunday bounds [%s, %s)", cs, ce)
	}
}

func TestISOWeekAtYearBoundary(t *testing.T) {
	// Friday 2 Jan 2026: last completed week is Mon 22 Dec → Mon 29 Dec 2025.
	s, e := LastCompleted(Week, time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC), time.UTC)
	if s.Format("2006-01-02") != "2025-12-22" || e.Format("2006-01-02") != "2025-12-29" {
		t.Errorf("got [%s, %s)", s, e)
	}
	// Monday 5 Jan 2026: the week spanning the new year, Mon 29 Dec 2025 → Mon 5 Jan 2026.
	s, e = LastCompleted(Week, time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), time.UTC)
	if s.Format("2006-01-02") != "2025-12-29" || e.Format("2006-01-02") != "2026-01-05" {
		t.Errorf("got [%s, %s)", s, e)
	}
	if y, w := s.ISOWeek(); y != 2026 || w != 1 {
		t.Errorf("ISO week of %s = %d-W%d, want 2026-W1", s, y, w)
	}
	if got := Name(Week, s); got != "Top 50 · Week of Dec 29, 2025" {
		t.Errorf("name = %q", got)
	}
	// January: last month is December of the previous year.
	ms, me := LastCompleted(Month, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), time.UTC)
	if ms.Format("2006-01-02") != "2025-12-01" || me.Format("2006-01-02") != "2026-01-01" {
		t.Errorf("month got [%s, %s)", ms, me)
	}
}

func TestBoundsAcrossDST(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	// DST ended on Sun 1 Nov 2026 (EDT → EST). The week Mon 26 Oct → Mon 2 Nov
	// is 7 days + 1 hour long, and both ends are local midnights.
	s, e := LastCompleted(Week, time.Date(2026, 11, 4, 12, 0, 0, 0, ny), ny)
	if s.Format(time.RFC3339) != "2026-10-26T00:00:00-04:00" || e.Format(time.RFC3339) != "2026-11-02T00:00:00-05:00" {
		t.Errorf("got [%s, %s)", s.Format(time.RFC3339), e.Format(time.RFC3339))
	}
	if d := e.Sub(s); d != 7*24*time.Hour+time.Hour {
		t.Errorf("week length = %s", d)
	}
	// DST started on Sun 8 Mar 2026: March is one hour short.
	ms, me := LastCompleted(Month, time.Date(2026, 4, 2, 0, 30, 0, 0, ny), ny)
	if ms.Format(time.RFC3339) != "2026-03-01T00:00:00-05:00" || me.Format(time.RFC3339) != "2026-04-01T00:00:00-04:00" {
		t.Errorf("month got [%s, %s)", ms.Format(time.RFC3339), me.Format(time.RFC3339))
	}
	// 03:30 UTC on 1 Apr is still 31 Mar in New York: March isn't over yet.
	ms, _ = LastCompleted(Month, time.Date(2026, 4, 1, 3, 30, 0, 0, time.UTC), ny)
	if ms.Format("2006-01") != "2026-02" {
		t.Errorf("zone not applied: last month = %s", ms.Format("2006-01"))
	}
	ys, ye := LastCompleted(Year, time.Date(2026, 1, 1, 4, 59, 0, 0, time.UTC), ny)
	if ys.Year() != 2024 || ye.Year() != 2025 {
		t.Errorf("year got [%s, %s)", ys, ye)
	}
}

func TestNamesAndLabels(t *testing.T) {
	w := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	m := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	y := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		p                  Period
		start              time.Time
		label, name, descr string
	}{
		{Week, w, "Week of Nov 2, 2026", "Top 50 · Week of Nov 2, 2026",
			"The 50 most-played tracks of the week of Nov 2, 2026, generated automatically from listening history."},
		{Month, m, "November 2026", "Top 50 · November 2026",
			"The 50 most-played tracks of November 2026, generated automatically from listening history."},
		{Year, y, "2026", "Top 50 · 2026",
			"The 50 most-played tracks of 2026, generated automatically from listening history."},
	}
	for _, c := range cases {
		if got := Label(c.p, c.start); got != c.label {
			t.Errorf("Label(%s) = %q, want %q", c.p, got, c.label)
		}
		if got := Name(c.p, c.start); got != c.name {
			t.Errorf("Name(%s) = %q, want %q", c.p, got, c.name)
		}
		if got := Description(c.p, c.start); got != c.descr {
			t.Errorf("Description(%s) = %q, want %q", c.p, got, c.descr)
		}
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig("week,month,year", "UTC")
	if err != nil || !cfg.Enabled() || len(cfg.Periods) != 3 || cfg.Location != time.UTC {
		t.Fatalf("defaults: %+v, %v", cfg, err)
	}
	cfg, err = ParseConfig(" Year , week,week ", "America/New_York")
	if err != nil || len(cfg.Periods) != 2 || cfg.Periods[0] != Week || cfg.Periods[1] != Year || !cfg.Has(Year) || cfg.Has(Month) {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	for _, off := range []string{"", "none", "OFF"} {
		cfg, err = ParseConfig(off, "UTC")
		if err != nil || cfg.Enabled() {
			t.Errorf("%q should disable: %+v, %v", off, cfg, err)
		}
	}
	if cfg, err = ParseConfig("week,daily", "UTC"); err == nil || cfg.Enabled() {
		t.Error("unknown period should fail and disable")
	}
	for _, bad := range []string{"Mars/Base", "Local"} {
		if cfg, err = ParseConfig("week", bad); err == nil || cfg.Enabled() {
			t.Errorf("zone %q should fail and disable", bad)
		}
	}
}

func TestLiveDescription(t *testing.T) {
	cases := map[Period]time.Time{
		Week: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), Month: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Year: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	want := map[Period]string{
		Week:  "The 50 most-played tracks of the week of Nov 2, 2026 so far, updated hourly from listening history.",
		Month: "The 50 most-played tracks of November 2026 so far, updated hourly from listening history.",
		Year:  "The 50 most-played tracks of 2026 so far, updated hourly from listening history.",
	}
	for p, start := range cases {
		if got := LiveDescription(p, start, time.Hour); got != want[p] {
			t.Errorf("LiveDescription(%s) = %q, want %q", p, got, want[p])
		}
	}
}

func TestLiveDescriptionCadence(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		time.Hour: "hourly", 0: "hourly", 24 * time.Hour: "daily", 2 * time.Hour: "every 2 hours",
		90 * time.Minute: "every 90 minutes", 10 * time.Minute: "every 10 minutes", 10*time.Minute + 30*time.Second: "every 10m30s",
	}
	for d, c := range cases {
		want := "The 50 most-played tracks of November 2026 so far, updated " + c + " from listening history."
		if got := LiveDescription(Month, start, d); got != want {
			t.Errorf("LiveDescription(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestParseLive(t *testing.T) {
	all := []Period{Week, Month, Year}
	live, ignored, err := ParseLive("week", all)
	if err != nil || len(live) != 1 || live[0] != Week || len(ignored) != 0 {
		t.Fatalf("default: %v %v %v", live, ignored, err)
	}
	for _, off := range []string{"", "none", " Off "} {
		if live, ignored, err = ParseLive(off, all); err != nil || len(live) != 0 || len(ignored) != 0 {
			t.Errorf("%q: %v %v %v", off, live, ignored, err)
		}
	}
	// Kinds that are not generated at all are ignored (and reported).
	live, ignored, err = ParseLive("year, week ,month", []Period{Week, Year})
	if err != nil || len(live) != 2 || live[0] != Week || live[1] != Year || len(ignored) != 1 || ignored[0] != Month {
		t.Fatalf("subset: %v %v %v", live, ignored, err)
	}
	if live, _, err = ParseLive("week,hourly", all); err == nil || len(live) != 0 {
		t.Errorf("invalid: %v %v", live, err)
	}
	cfg := Config{Periods: []Period{Week, Month}, Location: time.UTC, Live: []Period{Week, Year}}
	if !cfg.IsLive(Week) || cfg.IsLive(Month) || cfg.IsLive(Year) || cfg.Refresh() != time.Hour {
		t.Errorf("IsLive/Refresh wrong for %+v", cfg)
	}
}

func TestParseLiveRefresh(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		bad  bool
	}{
		{"", time.Hour, false}, {"1h", time.Hour, false}, {"10m", 10 * time.Minute, false},
		{"90m", 90 * time.Minute, false}, {"9m59s", time.Hour, true}, {"0", time.Hour, true},
		{"-1h", time.Hour, true}, {"soon", time.Hour, true},
	}
	for _, c := range cases {
		got, err := ParseLiveRefresh(c.in)
		if got != c.want || (err != nil) != c.bad {
			t.Errorf("ParseLiveRefresh(%q) = %s, %v", c.in, got, err)
		}
	}
}

func TestBackoff(t *testing.T) {
	cases := map[int]time.Duration{
		1: 5 * time.Minute, 2: 10 * time.Minute, 3: 20 * time.Minute, 4: 40 * time.Minute,
		6: 160 * time.Minute, 7: 320 * time.Minute, 8: 6 * time.Hour, 50: 6 * time.Hour,
	}
	for n, want := range cases {
		if got := Backoff(n); got != want {
			t.Errorf("Backoff(%d) = %s, want %s", n, got, want)
		}
	}
}
