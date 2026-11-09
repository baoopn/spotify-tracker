// Package topplaylists generates a public Spotify playlist of the 50 most
// played tracks of every completed week, month and year. Kinds listed in
// TOP_PLAYLISTS_LIVE also get a live playlist of the current period, kept up
// to date while listening and finalized in place when the period ends.
package topplaylists

import (
	"fmt"
	"slices"
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

const (
	// DefaultLiveRefresh is the default time between two refreshes of a live
	// playlist (TOP_PLAYLISTS_LIVE_REFRESH).
	DefaultLiveRefresh = time.Hour
	// MinLiveRefresh is the shortest accepted live refresh interval.
	MinLiveRefresh = 10 * time.Minute
)

// Config is the parsed feature configuration.
type Config struct {
	Periods  []Period // enabled kinds in display order; empty = disabled
	Location *time.Location
	// Live lists the enabled kinds whose current period gets a live
	// playlist (display order); empty = none.
	Live []Period
	// LiveRefresh is the time between two refreshes of a live playlist
	// (0 = DefaultLiveRefresh).
	LiveRefresh time.Duration
	// Keep is how many created playlists of each kind are kept (the live
	// one included); older ones are pruned. 0 = never prune.
	Keep int
	// Verify is how often published playlists are checked against the
	// owner's Spotify library (and restored when deleted). 0 = never.
	Verify time.Duration
	// Owner prefixes playlist names ("{Owner}'s Top 50 · …"). Empty = the
	// connected Spotify account's display name.
	Owner string
}

// IsLive reports whether the current period of kind p gets a live playlist.
func (c Config) IsLive(p Period) bool {
	return c.Has(p) && slices.Contains(c.Live, p)
}

// Refresh returns the live refresh interval.
func (c Config) Refresh() time.Duration {
	if c.LiveRefresh <= 0 {
		return DefaultLiveRefresh
	}
	return c.LiveRefresh
}

// Enabled reports whether any period kind is generated.
func (c Config) Enabled() bool { return len(c.Periods) > 0 && c.Location != nil }

// Has reports whether p is enabled.
func (c Config) Has(p Period) bool { return slices.Contains(c.Periods, p) }

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
func ParsePeriods(s string) ([]Period, error) { return parseKinds("TOP_PLAYLISTS_PERIODS", s) }

// ParseLive parses TOP_PLAYLISTS_LIVE (comma-separated week, month, year;
// "", "none" or "off" = no live playlists) against the enabled kinds: kinds
// that are not enabled are dropped from live and returned in ignored. On
// error live is empty.
func ParseLive(s string, enabled []Period) (live, ignored []Period, err error) {
	kinds, err := parseKinds("TOP_PLAYLISTS_LIVE", s)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range kinds {
		if slices.Contains(enabled, p) {
			live = append(live, p)
		} else {
			ignored = append(ignored, p)
		}
	}
	return live, ignored, nil
}

// ParseLiveRefresh parses TOP_PLAYLISTS_LIVE_REFRESH (a Go duration, at least
// MinLiveRefresh; "" = DefaultLiveRefresh). An invalid or too short value
// returns DefaultLiveRefresh together with the error.
func ParseLiveRefresh(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultLiveRefresh, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return DefaultLiveRefresh, fmt.Errorf("TOP_PLAYLISTS_LIVE_REFRESH: invalid duration %q: %w", s, err)
	}
	if d < MinLiveRefresh {
		return DefaultLiveRefresh, fmt.Errorf("TOP_PLAYLISTS_LIVE_REFRESH: %s is shorter than the minimum %s", d, MinLiveRefresh)
	}
	return d, nil
}

func parseKinds(name, s string) ([]Period, error) {
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
			return nil, fmt.Errorf("%s: unknown period %q (want week, month, year or none)", name, part)
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

// Name is the Spotify playlist name without an owner, e.g.
// "Top 50 · November 2026".
func Name(p Period, start time.Time) string { return "Top 50 · " + Label(p, start) }

// OwnerName is the Spotify playlist name of an owner, e.g.
// "alex's Top 50 · November 2026" (Name when owner is empty).
func OwnerName(owner string, p Period, start time.Time) string {
	if owner == "" {
		return Name(p, start)
	}
	return owner + "'s " + Name(p, start)
}

// Description is the Spotify playlist description of a finished period.
func Description(p Period, start time.Time) string {
	return "The 50 most-played tracks of " + descriptionOf(p, start) + ", generated automatically from listening history."
}

// LiveDescription is the Spotify playlist description of a live (unfinished)
// period refreshed every interval ("updated hourly" for 1h).
func LiveDescription(p Period, start time.Time, every time.Duration) string {
	return "The 50 most-played tracks of " + descriptionOf(p, start) + " so far, updated " +
		cadence(every) + " from listening history."
}

// cadence describes a refresh interval: "hourly", "daily", "every 2 hours",
// "every 90 minutes".
func cadence(d time.Duration) string {
	switch {
	case d <= 0 || d == time.Hour:
		return "hourly"
	case d == 24*time.Hour:
		return "daily"
	case d%time.Hour == 0:
		return fmt.Sprintf("every %d hours", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("every %d minutes", d/time.Minute)
	}
	return "every " + d.String()
}

func descriptionOf(p Period, start time.Time) string {
	if p == Week {
		return "the week of " + start.Format("Jan 2, 2006")
	}
	return Label(p, start)
}
