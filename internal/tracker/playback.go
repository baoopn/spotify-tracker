package tracker

import (
	"time"
)

// State is the last observed playback, persisted in Redis between polls so
// that restarts do not create duplicate history rows.
type State struct {
	TrackID    string
	ProgressMS *int32
}

// Decision describes what to write for a new snapshot.
type Decision struct {
	// NewHistory: append a listening_history row.
	NewHistory bool
	// Replay: increment replay_count of the latest history row.
	Replay bool
}

// Decide applies the history rules:
//
//   - A different track than last observed starts a new history entry.
//   - The same track whose progress jumped back by more than threshold is a
//     replay of the latest entry.
//   - Otherwise the snapshot continues the current entry.
//
// This mirrors the behaviour of the original Deno poller.
func Decide(prev State, trackID string, progressMS *int32, threshold time.Duration) Decision {
	if prev.TrackID != trackID {
		return Decision{NewHistory: true}
	}
	if prev.ProgressMS != nil && progressMS != nil &&
		int64(*progressMS)+threshold.Milliseconds() < int64(*prev.ProgressMS) {
		return Decision{Replay: true}
	}
	return Decision{}
}
