package poller

import (
	"slices"
	"time"
)

// Spotify's timestamp is the time of the last playback state change (play,
// pause, seek, skip), and stays fixed while a song plays. Within one state,
// base = progress_ms - (mid - timestamp) is almost constant (about ±25 ms
// measured), while progress_ms on its own wobbles by ±60..140 ms because the
// Spotify response time varies. The anchor publishes the median base of the
// recent polls plus the time since the state change, so the position the
// listeners follow moves smoothly.
const (
	anchorSamples = 15
	// anchorMaxJump: a raw position this far from the smoothed one is a
	// missed state change; the samples restart from it.
	anchorMaxJump = 1000
)

type anchorKey struct {
	trackID   string
	changedAt int64 // Spotify timestamp, unix ms
	playing   bool
}

// anchor smooths progress_ms for one playback state at a time. It is not
// safe for concurrent use (the poll loop owns it).
type anchor struct {
	key     anchorKey
	bases   []int64
	lastRaw *int64
}

func (a *anchor) reset() {
	a.key, a.bases, a.lastRaw = anchorKey{}, nil, nil
}

// smooth returns the progress to publish for a snapshot fetched at mid
// (the midpoint of the request, poller clock). changedAt is Spotify's
// timestamp (nil when absent). Paused snapshots, a missing timestamp or
// progress, progress that is not advancing while "playing" (a stale report;
// the past-the-end rule handles it) and a jump of over a second pass the raw
// value through.
func (a *anchor) smooth(trackID string, changedAt *time.Time, playing bool, raw *int32, durationMS int32, mid time.Time) *int32 {
	if raw == nil || changedAt == nil {
		a.reset()
		return raw
	}
	key := anchorKey{trackID: trackID, changedAt: changedAt.UnixMilli(), playing: playing}
	if key != a.key {
		a.reset()
		a.key = key
	}
	r := int64(*raw)
	stuck := a.lastRaw != nil && *a.lastRaw == r
	a.lastRaw = &r
	if !playing || stuck {
		a.bases = nil
		return raw
	}
	elapsed := mid.UnixMilli() - key.changedAt
	base := r - elapsed
	if len(a.bases) > 0 {
		if d := r - (median(a.bases) + elapsed); d > anchorMaxJump || d < -anchorMaxJump {
			a.bases = []int64{base}
			return raw
		}
	}
	a.bases = append(a.bases, base)
	if len(a.bases) > anchorSamples {
		a.bases = a.bases[len(a.bases)-anchorSamples:]
	}
	pos := max(0, median(a.bases)+elapsed)
	if durationMS > 0 {
		pos = min(pos, int64(durationMS))
	}
	out := int32(pos)
	return &out
}

// median of a non-empty list (the mean of the two middle values for an
// even count).
func median(v []int64) int64 {
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
