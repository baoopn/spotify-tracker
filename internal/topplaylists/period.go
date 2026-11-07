// Package topplaylists generates a public Spotify playlist of the 50 most
// played tracks of every completed week, month and year.
package topplaylists

import (
	"fmt"
	"strings"
	"time"
)

// Period is a kind of calendar period.
type Period string

// Period kinds, in display order.
const (
	Week  Period = "week"
	Month Period = "month"
	Year  Period = "year"
)

// AllPeriods lists every period kind in display order.
var AllPeriods = []Period{Week, Month, Year}

// TrackLimit is the size of a top playlist.
const TrackLimit = 50

// Config is the parsed feature configuration.
type Config struct {
	Periods  []Period // enabled kinds in display order; empty = disabled
	Location *time.Location
}

// Enabled reports whether any period kind is generated.
func (c Config) Enabled() bool { return len(c.Periods) > 0 && c.Location != nil }

// Has reports whether p is enabled.
func (c Config) Has(p Period) bool {
	for _, q := range c.Periods {
		if q == p {
			return true
		}
	}
	return false
}

// ParseConfig parses TOP_PLAYLISTS_PERIODS (comma-separated week, month,
// year; "", "none" or "off" disables the feature) and TOP_PLAYLISTS_TIME_ZONE
// (an IANA zone name). On error the returned Config is disabled.
func ParseConfig(periods, zone string) (Config, error) {
	ps, err := ParsePeriods(periods)
	if err != nil {
		return Config{}, err
	}
	loc, err := LoadZone(zone)
	if err != nil {
		return Config{}, err
	}
	return Config{Periods: ps, Location: loc}, nil
}

// ParsePeriods parses a comma-separated list of period kinds. The result is
// de-duplicated and in display order.
func ParsePeriods(s string) ([]Period, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "none" || s == "off" {
		return nil, nil
	}
	seen := map[Period]bool{}
	for _, part := range strings.Split(s, ",") {
		p := Period(strings.TrimSpace(part))
		if p == "" {
			continue
		}
		if p != Week && p != Month && p != Year {
			return nil, fmt.Errorf("TOP_PLAYLISTS_PERIODS: unknown period %q (want week, month, year or none)", part)
		}
		seen[p] = true
	}
	var out []Period
	for _, p := range AllPeriods {
		if seen[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

// LoadZone loads an IANA time zone. "Local" is rejected: it would make the
// period boundaries depend on the host.
func LoadZone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "UTC"
	}
	if name == "Local" {
		return nil, fmt.Errorf("TOP_PLAYLISTS_TIME_ZONE: use an IANA zone name, not %q", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("TOP_PLAYLISTS_TIME_ZONE: unknown time zone %q: %w", name, err)
	}
	return loc, nil
}

// Bounds returns the half-open period [start, end) of kind p that contains
// t, in loc. Weeks are ISO weeks (Monday to Monday). Boundaries are local
// midnights computed with time.Date, so DST changes are handled.
func Bounds(p Period, t time.Time, loc *time.Location) (start, end time.Time) {
	t = t.In(loc)
	y, m, d := t.Date()
	switch p {
	case Week:
		back := (int(t.Weekday()) + 6) % 7 // days since Monday
		return time.Date(y, m, d-back, 0, 0, 0, 0, loc), time.Date(y, m, d-back+7, 0, 0, 0, 0, loc)
	case Month:
		return time.Date(y, m, 1, 0, 0, 0, 0, loc), time.Date(y, m+1, 1, 0, 0, 0, 0, loc)
	default:
		return time.Date(y, 1, 1, 0, 0, 0, 0, loc), time.Date(y+1, 1, 1, 0, 0, 0, 0, loc)
	}
}

// LastCompleted returns the most recently completed period of kind p
// before now, in loc.
func LastCompleted(p Period, now time.Time, loc *time.Location) (start, end time.Time) {
	cur, _ := Bounds(p, now, loc)
	y, m, d := cur.Date()
	switch p {
	case Week:
		start = time.Date(y, m, d-7, 0, 0, 0, 0, loc)
	case Month:
		start = time.Date(y, m-1, 1, 0, 0, 0, 0, loc)
	default:
		start = time.Date(y-1, 1, 1, 0, 0, 0, 0, loc)
	}
	return start, cur
}

// Label names a period: "Week of Nov 2, 2026", "November 2026" or "2026".
// start must be in the period's time zone.
func Label(p Period, start time.Time) string {
	switch p {
	case Week:
		return "Week of " + start.Format("Jan 2, 2006")
	case Month:
		return start.Format("January 2006")
	default:
		return start.Format("2006")
	}
}

// Name is the Spotify playlist name, e.g. "Top 50 · November 2026".
func Name(p Period, start time.Time) string { return "Top 50 · " + Label(p, start) }

// Description is the Spotify playlist description.
func Description(p Period, start time.Time) string {
	of := Label(p, start)
	if p == Week {
		of = "the week of " + start.Format("Jan 2, 2006")
	}
	return "The 50 most-played tracks of " + of + ", generated automatically from listening history."
}
