// Package backupstatus computes the health of the off-site backup service from
// the status rows the backup container writes to the database.
package backupstatus

import (
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// Health is the backup service's overall state.
type Health string

// Health values, as shown in the admin dashboard.
const (
	OK           Health = "ok"
	Stale        Health = "stale"
	Failing      Health = "failing"
	Unconfigured Health = "unconfigured"
	Offline      Health = "offline"
	Paused       Health = "paused"
	Unknown      Health = "unknown"
)

const (
	// OfflineAfter is how long without a heartbeat the agent counts as
	// offline (it heartbeats every minute).
	OfflineAfter = 5 * time.Minute
	// StaleAfter is the maximum age of the last successful backup. Keep in
	// sync with the BACKUP_MAX_AGE_HOURS default of the backup container.
	StaleAfter = 26 * time.Hour
)

// State is what the health is computed from.
type State struct {
	// Agent is the heartbeat row (nil: the backup service never ran).
	Agent       *domain.BackupAgent
	LastSuccess *domain.BackupRun
	LastFailure *domain.BackupRun
	// FirstRunAt is the start of the oldest recorded run (nil: none).
	FirstRunAt *time.Time
}

// RunTime is when a run ended (or started, while it runs).
func RunTime(r *domain.BackupRun) time.Time {
	if r.FinishedAt != nil {
		return *r.FinishedAt
	}
	return r.StartedAt
}

// Compute returns the health at now and a one-line explanation. The order
// is: unknown, offline, unconfigured, paused, failing, stale, ok.
func Compute(s State, now time.Time) (Health, string) {
	a := s.Agent
	switch {
	case a == nil:
		return Unknown, "backup service not running"
	case now.Sub(a.LastSeenAt) > OfflineAfter:
		return Offline, "no heartbeat from the backup service since " + stamp(a.LastSeenAt)
	case !a.Configured:
		if a.ConfigError != nil && *a.ConfigError != "" {
			return Unconfigured, *a.ConfigError
		}
		return Unconfigured, "off-site backups are not set up"
	case a.Paused:
		return Paused, "scheduled backups are paused"
	}
	if f := s.LastFailure; f != nil && (s.LastSuccess == nil || RunTime(f).After(RunTime(s.LastSuccess))) {
		return Failing, "the last backup failed at " + stamp(RunTime(f))
	}
	if ok := s.LastSuccess; ok != nil {
		if now.Sub(RunTime(ok)) > StaleAfter {
			return Stale, "the last successful backup is from " + stamp(RunTime(ok))
		}
		return OK, "last backup " + stamp(RunTime(ok))
	}
	if s.FirstRunAt != nil && now.Sub(*s.FirstRunAt) > StaleAfter {
		return Stale, "no backup has succeeded yet"
	}
	return OK, "no backup yet"
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }
