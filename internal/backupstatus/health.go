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

// InterruptedAfter is how long a run may stay 'running' before it counts
// as failed (interrupted). Requests claimed longer ago no longer count as
// pending either.
const InterruptedAfter = 2 * time.Hour

// OverdueGrace is how late a scheduled run may start before the schedule
// counts as missed: the retry delay (30 min) plus an hour.
const OverdueGrace = 30*time.Minute + time.Hour

// State is what the health is computed from.
type State struct {
	// Agent is the heartbeat row (nil: the backup service never ran).
	Agent       *domain.BackupAgent
	LastSuccess *domain.BackupRun
	LastFailure *domain.BackupRun
	// LastRunning is the newest run still marked running (nil: none).
	LastRunning *domain.BackupRun
	// FirstRunAt and LastRunAt are the start of the oldest and the newest
	// recorded run (nil: none).
	FirstRunAt *time.Time
	LastRunAt  *time.Time
}

// RunTime is when a run ended (or started, while it runs).
func RunTime(r *domain.BackupRun) time.Time {
	if r.FinishedAt != nil {
		return *r.FinishedAt
	}
	return r.StartedAt
}

// Failure is the newest failed run at now that has no success started
// after it, or nil. A run still 'running' after InterruptedAfter counts as
// failed. Runs are ordered by start time, so a stale row marked failed late
// never looks newer than a later success.
func (s State) Failure(now time.Time) *domain.BackupRun {
	f := s.LastFailure
	if r := s.LastRunning; r != nil && now.Sub(r.StartedAt) > InterruptedAfter &&
		(f == nil || r.StartedAt.After(f.StartedAt)) {
		f = r
	}
	if f == nil || (s.LastSuccess != nil && !f.StartedAt.After(s.LastSuccess.StartedAt)) {
		return nil
	}
	return f
}

// Overdue reports whether the agent's scheduled run is more than
// OverdueGrace late and no run started since it was due.
func (s State) Overdue(now time.Time) bool {
	a := s.Agent
	if a == nil || !a.Configured || a.Paused || a.NextRunAt == nil || !a.NextRunAt.Before(now.Add(-OverdueGrace)) {
		return false
	}
	return s.LastRunAt == nil || s.LastRunAt.Before(*a.NextRunAt)
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
	if f := s.Failure(now); f != nil {
		if f.Status == domain.BackupRunning {
			return Failing, "the backup started at " + stamp(f.StartedAt) + " was interrupted"
		}
		return Failing, "the last backup failed at " + stamp(RunTime(f))
	}
	if s.Overdue(now) {
		return Stale, "scheduled backup did not run"
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
