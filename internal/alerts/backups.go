package alerts

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/baoopn/spotify-tracker/internal/backupstatus"
	"github.com/baoopn/spotify-tracker/internal/notify"
)

// BackupOfflineAlertAfter is how long the backup service may be silent
// before an alert is sent.
const BackupOfflineAlertAfter = time.Hour

const (
	backupHealthTTL  = 24 * time.Hour
	backupOfflineTTL = 30 * 24 * time.Hour
)

// BackupSource loads the backup status (implemented by *store.Store).
type BackupSource interface {
	BackupState(ctx context.Context) (backupstatus.State, error)
}

// WatchBackups makes the checker alert on backup problems. statusURL is the
// admin backup status page.
func (c *Checker) WatchBackups(src BackupSource, statusURL string) {
	c.backups, c.backupURL = src, statusURL
}

// CheckBackups sends an alert when backups fail, are stale or the backup
// service went offline.
func (c *Checker) CheckBackups(ctx context.Context) {
	if c.backups == nil {
		return
	}
	s, err := c.backups.BackupState(ctx)
	if err != nil {
		c.log.WarnContext(ctx, "backup check: load backup status failed", slog.Any("error", err))
		return
	}
	key, ttl, e, ok := BackupAlert(s, c.clock(), c.backupURL)
	if !ok {
		return
	}
	c.log.WarnContext(ctx, "backup problem", slog.String("subject", e.Subject))
	c.notifier.Once(ctx, key, ttl, e)
}

// BackupAlert picks the alert for state s at now: a dedupe key, its TTL and
// the email. ok is false when nothing is wrong, the service was never set
// up, or backups are paused. Failing and stale alerts repeat at most once
// a day per state; an offline alert is sent once per outage.
func BackupAlert(s backupstatus.State, now time.Time, statusURL string) (key string, ttl time.Duration, e notify.Email, ok bool) {
	health, _ := backupstatus.Compute(s, now)
	day := now.UTC().Format("2006-01-02")
	switch health {
	case backupstatus.Offline:
		a := s.Agent
		// Never set up or paused before it went away: nothing to warn about.
		if !a.Configured || a.Paused || now.Sub(a.LastSeenAt) <= BackupOfflineAlertAfter {
			return "", 0, notify.Email{}, false
		}
		return "backup-offline:" + strconv.FormatInt(a.LastSeenAt.Unix(), 10), backupOfflineTTL,
			BackupOfflineEmail(a.LastSeenAt, statusURL, now), true
	case backupstatus.Failing:
		return "backup-health:failing:" + day, backupHealthTTL,
			BackupFailingEmail(s.LastFailure, s.LastSuccess, statusURL, now), true
	case backupstatus.Stale:
		return "backup-health:stale:" + day, backupHealthTTL,
			BackupStaleEmail(s.LastSuccess, s.Agent, statusURL, now), true
	}
	return "", 0, notify.Email{}, false
}
