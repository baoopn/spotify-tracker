package alerts

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/backupstatus"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/notify"
)

const statusURL = "https://listening.example.com/admin/settings/backups"

// fakeSender emulates notify.Notifier.Once: a key is sent once per TTL.
type fakeSender struct {
	now  func() time.Time
	keys map[string]time.Time // key → expiry
	sent []notify.Email
}

func (f *fakeSender) Once(_ context.Context, key string, ttl time.Duration, e notify.Email) {
	if exp, ok := f.keys[key]; ok && f.now().Before(exp) {
		return
	}
	f.keys[key] = f.now().Add(ttl)
	f.sent = append(f.sent, e)
}

type fakeBackupSource struct {
	s   backupstatus.State
	err error
}

func (f *fakeBackupSource) BackupState(context.Context) (backupstatus.State, error) {
	return f.s, f.err
}

func backupRun(status string, end time.Time) *domain.BackupRun {
	msg := "pg_dump: connection refused"
	r := &domain.BackupRun{Trigger: "schedule", Status: status, StartedAt: end.Add(-time.Minute), FinishedAt: &end}
	if status == domain.BackupFailed {
		r.Error = &msg
	}
	return r
}

func TestBackupAlert(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	seen := func(d time.Duration, mod func(*domain.BackupAgent)) *domain.BackupAgent {
		a := &domain.BackupAgent{LastSeenAt: now.Add(-d), Configured: true}
		if mod != nil {
			mod(a)
		}
		return a
	}
	tests := []struct {
		name    string
		s       backupstatus.State
		key     string
		subject string
	}{
		{"never set up", backupstatus.State{}, "", ""},
		{"healthy", backupstatus.State{Agent: seen(time.Minute, nil), LastSuccess: backupRun("success", now.Add(-time.Hour))}, "", ""},
		{"unconfigured", backupstatus.State{Agent: seen(time.Minute, func(a *domain.BackupAgent) { a.Configured = false })}, "", ""},
		{"paused and failing", backupstatus.State{Agent: seen(time.Minute, func(a *domain.BackupAgent) { a.Paused = true }),
			LastFailure: backupRun("failed", now.Add(-time.Hour))}, "", ""},
		{"offline 30 min", backupstatus.State{Agent: seen(30*time.Minute, nil)}, "", ""},
		{"offline 2 h", backupstatus.State{Agent: seen(2*time.Hour, nil)},
			"backup-offline:" + strconv.FormatInt(now.Add(-2*time.Hour).Unix(), 10), "[spotify-tracker] Backup service is offline"},
		{"offline 2 h, was unconfigured", backupstatus.State{Agent: seen(2*time.Hour, func(a *domain.BackupAgent) { a.Configured = false })}, "", ""},
		{"offline 2 h, was paused", backupstatus.State{Agent: seen(2*time.Hour, func(a *domain.BackupAgent) { a.Paused = true })}, "", ""},
		{"failing", backupstatus.State{Agent: seen(time.Minute, nil), LastFailure: backupRun("failed", now.Add(-time.Hour))},
			"backup-health:failing:2026-11-09", "[spotify-tracker] Backup failed"},
		{"stale", backupstatus.State{Agent: seen(time.Minute, nil), LastSuccess: backupRun("success", now.Add(-30*time.Hour))},
			"backup-health:stale:2026-11-09", "[spotify-tracker] No recent backup"},
	}
	for _, tt := range tests {
		key, ttl, e, ok := BackupAlert(tt.s, now, statusURL)
		if ok != (tt.key != "") || key != tt.key || e.Subject != tt.subject {
			t.Errorf("%s: key %q ok %v subject %q; want %q %q", tt.name, key, ok, e.Subject, tt.key, tt.subject)
			continue
		}
		if !ok {
			continue
		}
		if ttl < 24*time.Hour {
			t.Errorf("%s: ttl %s", tt.name, ttl)
		}
		if e.Action == nil || e.Action.URL != statusURL || e.Action.Label != "Open backup status" {
			t.Errorf("%s: action %+v", tt.name, e.Action)
		}
	}
}

func TestCheckBackupsDedupe(t *testing.T) {
	now := time.Date(2026, 11, 9, 1, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	snd := &fakeSender{now: clock, keys: map[string]time.Time{}}
	src := &fakeBackupSource{s: backupstatus.State{
		Agent:       &domain.BackupAgent{LastSeenAt: now, Configured: true},
		LastFailure: backupRun("failed", now.Add(-time.Hour)),
	}}
	c := &Checker{notifier: snd, log: slog.New(slog.DiscardHandler), now: clock}
	c.CheckBackups(context.Background()) // not watched: no-op
	if len(snd.sent) != 0 {
		t.Fatal("sent without WatchBackups")
	}
	c.WatchBackups(src, statusURL)

	check := func(advance time.Duration) {
		now = now.Add(advance)
		src.s.Agent.LastSeenAt = now
		c.CheckBackups(context.Background())
	}
	check(0)
	check(time.Hour) // same day, same state
	if len(snd.sent) != 1 || snd.sent[0].Tone != notify.Danger {
		t.Fatalf("failing alerts %d", len(snd.sent))
	}
	src.s.LastSuccess = backupRun("success", now.Add(-28*time.Hour))
	src.s.LastFailure = backupRun("failed", now.Add(-29*time.Hour))
	check(time.Hour) // stale is a new state
	if len(snd.sent) != 2 || snd.sent[1].Tone != notify.Warning {
		t.Fatalf("stale alerts %d", len(snd.sent))
	}
	check(24 * time.Hour) // next day: repeat
	if len(snd.sent) != 3 {
		t.Fatalf("next-day alerts %d", len(snd.sent))
	}

	// Offline: once per outage, however long it lasts.
	for range 5 {
		now = now.Add(2 * time.Hour)
		c.CheckBackups(context.Background())
	}
	if len(snd.sent) != 4 || !strings.Contains(snd.sent[3].Subject, "offline") {
		t.Fatalf("offline alerts %d", len(snd.sent))
	}

	src.err = errors.New("db down")
	check(time.Hour)
	if len(snd.sent) != 4 {
		t.Fatal("alert sent on a load error")
	}
}

func TestBackupEmails(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	f := BackupFailingEmail(backupRun("failed", now.Add(-2*time.Hour)), nil, statusURL, now)
	if f.Tone != notify.Danger || f.Code != "pg_dump: connection refused" {
		t.Errorf("failing: %+v", f)
	}
	if last := f.Facts[len(f.Facts)-1]; last.Value != "none yet" {
		t.Errorf("failing last success: %+v", last)
	}
	if e := BackupFailingEmail(nil, nil, statusURL, now); e.Code != "" || len(e.Facts) != 1 {
		t.Errorf("failing without run: %+v", e)
	}
	s := BackupStaleEmail(backupRun("success", now.Add(-50*time.Hour)), nil, statusURL, now)
	if s.Tone != notify.Warning || len(s.Facts) != 1 || !strings.HasSuffix(s.Facts[0].Value, "(2 days ago)") {
		t.Errorf("stale: %+v", s)
	}
	o := BackupOfflineEmail(now.Add(-3*time.Hour), statusURL, now)
	if o.Tone != notify.Warning || o.Facts[0].Value != "2026-11-09 09:00 UTC (less than a day ago)" {
		t.Errorf("offline: %+v", o)
	}
	for _, e := range []notify.Email{f, s, o} {
		if _, _, err := e.Render(notify.Brand{}); err != nil {
			t.Errorf("%s: %v", e.Subject, err)
		}
	}
}
