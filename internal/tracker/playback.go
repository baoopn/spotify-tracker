package tracker

import (
	"time"
)

// Defaults for the play-count rules (see PlayRules).
const (
	DefaultMinFraction  = 0.3
	DefaultResumeWindow = 24 * time.Hour

	// fallbackNeedMS is the listening time that counts a play when the
	// track's duration is unknown.
	fallbackNeedMS = 30_000
	// stepSlackMS lets a progress step run slightly ahead of the wall clock
	// (Spotify's progress and our fetch times are not exact).
	stepSlackMS = 2_000
	// maxStepMS caps the listening time one snapshot can add, so a seek made
	// while no snapshots arrived (paused, idle, rate limited) is not counted.
	maxStepMS = 30_000
	// nearStartMS and nearStartFraction define "back at the start": under
	// 10 s or under 5% of the duration.
	nearStartMS       = 10_000
	nearStartFraction = 0.05
)

// PlayRules configures when a listen counts as a play.
type PlayRules struct {
	// MinFraction of the duration that must be heard before a listen counts.
	MinFraction float64
	// ResumeWindow is how long after the last snapshot the same track can
	// resume as the same listen. After it, a resume starts a new listen.
	ResumeWindow time.Duration
	// ReplayThreshold is how far the progress must jump back on a counted
	// listen to start the next listen of the same track.
	ReplayThreshold time.Duration
}

func (r PlayRules) withDefaults() PlayRules {
	if r.MinFraction <= 0 || r.MinFraction > 1 {
		r.MinFraction = DefaultMinFraction
	}
	if r.ResumeWindow <= 0 {
		r.ResumeWindow = DefaultResumeWindow
	}
	if r.ReplayThreshold <= 0 {
		r.ReplayThreshold = 10 * time.Second
	}
	return r
}

// Listen is the listen in progress, persisted in Redis between snapshots so
// that restarts and idle periods neither lose nor double-count it.
type Listen struct {
	TrackID string
	// StartedAt becomes the history row's played_at when the listen counts.
	StartedAt time.Time
	// ProgressMS is the last observed progress (nil when unknown).
	ProgressMS *int32
	// ListenedMS is the listening time heard in this listen.
	ListenedMS int64
	// Counted is set once the listen has been written as a play.
	Counted bool
	// Replay marks a listen that follows a counted listen of the same track:
	// it counts as a replay of the latest history row, not a new row.
	Replay bool
	// LastSeen is when the last snapshot of this listen arrived.
	LastSeen time.Time
}

// Snapshot is the part of a playback snapshot the rules look at.
type Snapshot struct {
	TrackID    string
	ProgressMS *int32
	DurationMS int32
	IsPlaying  bool
	// PlayedAt is Spotify's timestamp for the playback; At is when it was fetched.
	PlayedAt time.Time
	At       time.Time
}

// Decision describes what to write for a snapshot.
type Decision struct {
	// NewHistory: append a listening_history row with PlayedAt.
	NewHistory bool
	// Replay: increment replay_count of the latest history row.
	Replay bool
	// PlayedAt is the start of the listen being counted.
	PlayedAt time.Time
}

// Step applies the play-count rules to one snapshot and returns the updated
// listen and what to write:
//
//   - A listen counts once, when at least MinFraction of the duration (30 s
//     when unknown) has been heard. Its played_at is when it started.
//   - Listening time grows only by forward progress while playing, capped by
//     the wall-clock time since the last snapshot, so seeking forward adds
//     nothing.
//   - The same track continuing after a pause or idle period (within
//     ResumeWindow) is the same listen: it is never counted twice.
//   - Going back to the start, or jumping back by more than ReplayThreshold
//     after the listen counted, starts a new listen of the same track, which
//     counts as a replay. A small seek back does nothing.
//   - A different track, or the same one after ResumeWindow, starts a new
//     listen. An uncounted listen is dropped.
func Step(prev Listen, s Snapshot, r PlayRules) (Listen, Decision) {
	r = r.withDefaults()
	cur := prev
	switch {
	case prev.TrackID == "" || prev.TrackID != s.TrackID || s.At.Sub(prev.LastSeen) > r.ResumeWindow:
		cur = newListen(prev, s, false)
	case restarted(prev, s, r):
		cur = newListen(prev, s, prev.Counted || prev.Replay)
	default:
		cur.ListenedMS += heard(prev, s)
		if s.ProgressMS != nil {
			cur.ProgressMS = s.ProgressMS
		}
		cur.LastSeen = s.At
	}

	if cur.Counted || cur.ListenedMS < needMS(s.DurationMS, r.MinFraction) {
		return cur, Decision{}
	}
	cur.Counted = true
	if cur.Replay {
		return cur, Decision{Replay: true, PlayedAt: cur.StartedAt}
	}
	return cur, Decision{NewHistory: true, PlayedAt: cur.StartedAt}
}

// newListen starts a listen of s.TrackID. StartedAt is Spotify's timestamp,
// moved to the fetch time when it is not after the start of the previous
// listen: a stale timestamp could repeat an earlier played_at, and the
// (track_id, played_at) key would drop the new row.
func newListen(prev Listen, s Snapshot, replay bool) Listen {
	start := s.PlayedAt
	if start.IsZero() {
		start = s.At
	}
	if prev.TrackID != "" && !start.After(prev.StartedAt) {
		start = s.At
	}
	return Listen{TrackID: s.TrackID, StartedAt: start, ProgressMS: s.ProgressMS, Replay: replay, LastSeen: s.At}
}

// restarted reports whether the same track started over: progress went back
// to the start from past it, or jumped back by more than the replay threshold
// on a counted listen.
func restarted(prev Listen, s Snapshot, r PlayRules) bool {
	if prev.ProgressMS == nil || s.ProgressMS == nil {
		return false
	}
	back := int64(*prev.ProgressMS) - int64(*s.ProgressMS)
	if back <= r.ReplayThreshold.Milliseconds() {
		return false
	}
	return prev.Counted || nearStart(*s.ProgressMS, s.DurationMS)
}

func nearStart(progressMS, durationMS int32) bool {
	return progressMS < nearStartMS || (durationMS > 0 && float64(progressMS) < nearStartFraction*float64(durationMS))
}

// heard is the listening time between prev and s: forward progress while
// playing, capped by the wall-clock gap (plus slack) and maxStepMS.
func heard(prev Listen, s Snapshot) int64 {
	if !s.IsPlaying || prev.ProgressMS == nil || s.ProgressMS == nil {
		return 0
	}
	delta := int64(*s.ProgressMS) - int64(*prev.ProgressMS)
	if delta <= 0 {
		return 0
	}
	limit := s.At.Sub(prev.LastSeen).Milliseconds() + stepSlackMS
	return min(delta, limit, maxStepMS)
}

// needMS is the listening time a listen needs to count.
func needMS(durationMS int32, fraction float64) int64 {
	if durationMS <= 0 {
		return fallbackNeedMS
	}
	need := int64(fraction * float64(durationMS))
	return max(need, 1)
}
