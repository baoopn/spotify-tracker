package backupstatus

import (
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

func TestCompute(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	ptr := func(t time.Time) *time.Time { return &t }
	run := func(status string, startedAgo time.Duration) *domain.BackupRun {
		s := ago(startedAgo)
		return &domain.BackupRun{Status: status, StartedAt: s, FinishedAt: ptr(s.Add(time.Minute))}
	}
	agent := func(mod func(*domain.BackupAgent)) *domain.BackupAgent {
		a := &domain.BackupAgent{LastSeenAt: ago(time.Minute), Configured: true}
		if mod != nil {
			mod(a)
		}
		return a
	}
	cfgErr := "BACKUP_AGE_RECIPIENT is missing"
	tests := []struct {
		name string
		s    State
		want Health
	}{
		{"no agent row", State{}, Unknown},
		{"heartbeat 6 min old", State{Agent: agent(func(a *domain.BackupAgent) { a.LastSeenAt = ago(6 * time.Minute) })}, Offline},
		{"offline wins over failing", State{Agent: agent(func(a *domain.BackupAgent) { a.LastSeenAt = ago(time.Hour) }),
			LastFailure: run("failed", time.Hour)}, Offline},
		{"heartbeat 4 min old", State{Agent: agent(func(a *domain.BackupAgent) { a.LastSeenAt = ago(4 * time.Minute) }),
			LastSuccess: run("success", time.Hour)}, OK},
		{"unconfigured", State{Agent: agent(func(a *domain.BackupAgent) { a.Configured = false; a.ConfigError = &cfgErr })}, Unconfigured},
		{"paused", State{Agent: agent(func(a *domain.BackupAgent) { a.Paused = true }), LastFailure: run("failed", time.Hour)}, Paused},
		{"failed, never succeeded", State{Agent: agent(nil), LastFailure: run("failed", time.Hour)}, Failing},
		{"failed after success", State{Agent: agent(nil), LastSuccess: run("success", 25*time.Hour),
			LastFailure: run("failed", time.Hour)}, Failing},
		{"success after failure", State{Agent: agent(nil), LastSuccess: run("success", time.Hour),
			LastFailure: run("failed", 2*time.Hour)}, OK},
		{"success 27 h old", State{Agent: agent(nil), LastSuccess: run("success", 27*time.Hour)}, Stale},
		{"success 25 h old", State{Agent: agent(nil), LastSuccess: run("success", 25*time.Hour)}, OK},
		{"never ran", State{Agent: agent(nil)}, OK},
		{"first run 30 h ago, none succeeded", State{Agent: agent(nil), FirstRunAt: ptr(ago(30 * time.Hour))}, Stale},
		{"first run 2 h ago, none succeeded", State{Agent: agent(nil), FirstRunAt: ptr(ago(2 * time.Hour))}, OK},
	}
	for _, tt := range tests {
		got, hint := Compute(tt.s, now)
		if got != tt.want {
			t.Errorf("%s: health %q, want %q (%s)", tt.name, got, tt.want, hint)
		}
		if hint == "" {
			t.Errorf("%s: empty hint", tt.name)
		}
	}
	if _, hint := Compute(State{}, now); hint != "backup service not running" {
		t.Errorf("unknown hint %q", hint)
	}
	if _, hint := Compute(State{Agent: agent(func(a *domain.BackupAgent) { a.Configured = false; a.ConfigError = &cfgErr })}, now); hint != cfgErr {
		t.Errorf("unconfigured hint %q", hint)
	}
}

func TestRunTime(t *testing.T) {
	s := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	if got := RunTime(&domain.BackupRun{StartedAt: s}); !got.Equal(s) {
		t.Errorf("running: %v", got)
	}
	f := s.Add(time.Minute)
	if got := RunTime(&domain.BackupRun{StartedAt: s, FinishedAt: &f}); !got.Equal(f) {
		t.Errorf("finished: %v", got)
	}
}

func TestComputeRunsAndSchedule(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { x := now.Add(-d); return &x }
	agent := func(next *time.Time) *domain.BackupAgent {
		return &domain.BackupAgent{LastSeenAt: now, Configured: true, NextRunAt: next}
	}
	run := func(status string, started time.Duration, finished *time.Time) *domain.BackupRun {
		return &domain.BackupRun{Status: status, Trigger: "schedule", StartedAt: now.Add(-started), FinishedAt: finished}
	}
	tests := []struct {
		name   string
		s      State
		want   Health
		detail string
	}{
		{"interrupted row marked failed after a later success",
			State{Agent: agent(nil), LastSuccess: run("success", 3*time.Hour, at(170*time.Minute)),
				LastFailure: run("failed", 5*time.Hour, at(time.Hour))}, OK, ""},
		{"running for 3 h counts as failed",
			State{Agent: agent(nil), LastSuccess: run("success", 20*time.Hour, at(19*time.Hour)),
				LastRunning: run("running", 3*time.Hour, nil)}, Failing, "the backup started at 2026-11-09 09:00 UTC was interrupted"},
		{"running for 1 h is fine",
			State{Agent: agent(nil), LastSuccess: run("success", 20*time.Hour, at(19*time.Hour)),
				LastRunning: run("running", time.Hour, nil)}, OK, ""},
		{"old running row before a success",
			State{Agent: agent(nil), LastSuccess: run("success", 2*time.Hour, at(110*time.Minute)),
				LastRunning: run("running", 30*time.Hour, nil)}, OK, ""},
		{"scheduled run missed",
			State{Agent: agent(at(2 * time.Hour)), LastSuccess: run("success", 20*time.Hour, at(19*time.Hour)),
				LastRunAt: at(20 * time.Hour)}, Stale, "scheduled backup did not run"},
		{"scheduled run 1 h late is within grace",
			State{Agent: agent(at(time.Hour)), LastSuccess: run("success", 20*time.Hour, at(19*time.Hour)),
				LastRunAt: at(20 * time.Hour)}, OK, ""},
		{"scheduled run started after it was due",
			State{Agent: agent(at(2 * time.Hour)), LastSuccess: run("success", 20*time.Hour, at(19*time.Hour)),
				LastRunAt: at(110 * time.Minute)}, OK, ""},
	}
	for _, tt := range tests {
		got, detail := Compute(tt.s, now)
		if got != tt.want || (tt.detail != "" && detail != tt.detail) {
			t.Errorf("%s: %q %q, want %q %q", tt.name, got, detail, tt.want, tt.detail)
		}
	}
	paused := State{Agent: agent(at(5 * time.Hour))}
	paused.Agent.Paused = true
	if paused.Overdue(now) {
		t.Error("paused agent overdue")
	}
}
