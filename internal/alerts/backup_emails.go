package alerts

import (
	"time"

	"github.com/baoopn/spotify-tracker/internal/backupstatus"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/notify"
)

const backupStatusLabel = "Open backup status"

// BackupStatusPath is the admin page with the backup status.
const BackupStatusPath = "/admin/settings/backups"

func stampUTC(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func lastSuccessFact(ok *domain.BackupRun, now time.Time) notify.Fact {
	if ok == nil {
		return notify.Fact{Label: "Last good backup", Value: "none yet"}
	}
	at := backupstatus.RunTime(ok)
	return notify.Fact{Label: "Last good backup", Value: stampUTC(at) + " (" + relative(at, now) + ")"}
}

// BackupFailingEmail is the alert for a failed backup with no success since.
func BackupFailingEmail(failed, lastSuccess *domain.BackupRun, statusURL string, now time.Time) notify.Email {
	e := notify.Email{
		Subject:   "[spotify-tracker] Backup failed",
		Preheader: "The last off-site backup failed. Check the backup status page.",
		Tone:      notify.Danger,
		Badge:     "Backup failed",
		Title:     "The last backup failed",
		Paragraphs: []string{
			"The backup service tried to save an off-site copy of the database and could not. Listening history is still recorded, but there is no fresh backup of it.",
			"Open the backup status page to see what went wrong. Once it is fixed, run a backup by hand.",
		},
		Action: &notify.Action{Label: backupStatusLabel, URL: statusURL},
		Note:   "The backup service tries again on its next scheduled run.",
	}
	if failed != nil {
		at := backupstatus.RunTime(failed)
		e.Facts = append(e.Facts,
			notify.Fact{Label: "Failed", Value: stampUTC(at) + " (" + relative(at, now) + ")"},
			notify.Fact{Label: "Trigger", Value: failed.Trigger})
		if failed.Error != nil && *failed.Error != "" {
			e.Paragraphs[1] = "Open the backup status page for details. Once it is fixed, run a backup by hand. The backup service said:"
			e.Code = *failed.Error
		}
	}
	e.Facts = append(e.Facts, lastSuccessFact(lastSuccess, now))
	return e
}

// BackupStaleEmail is the alert for no successful backup in too long.
func BackupStaleEmail(lastSuccess *domain.BackupRun, agent *domain.BackupAgent, statusURL string, now time.Time) notify.Email {
	facts := []notify.Fact{lastSuccessFact(lastSuccess, now)}
	if agent != nil && agent.BackupTime != nil && *agent.BackupTime != "" {
		facts = append(facts, notify.Fact{Label: "Schedule", Value: "daily at " + *agent.BackupTime + " UTC"})
	}
	return notify.Email{
		Subject:   "[spotify-tracker] No recent backup",
		Preheader: "No off-site backup has succeeded in the last day.",
		Tone:      notify.Warning,
		Badge:     "Backup overdue",
		Title:     "Your backups are out of date",
		Paragraphs: []string{
			"The backup service runs once a day, but no backup has succeeded in more than 26 hours.",
			"Open the backup status page to see what the service is doing.",
		},
		Facts:  facts,
		Action: &notify.Action{Label: backupStatusLabel, URL: statusURL},
	}
}

// BackupOfflineEmail is the alert for a backup service that stopped
// reporting.
func BackupOfflineEmail(lastSeen time.Time, statusURL string, now time.Time) notify.Email {
	return notify.Email{
		Subject:   "[spotify-tracker] Backup service is offline",
		Preheader: "The backup service stopped reporting. Backups may not run.",
		Tone:      notify.Warning,
		Badge:     "Offline",
		Title:     "The backup service is offline",
		Paragraphs: []string{
			"The backup service checks in every minute. It has not checked in for more than an hour, so backups may not run.",
			"Check that the backup container is running.",
		},
		Facts:  []notify.Fact{{Label: "Last seen", Value: stampUTC(lastSeen) + " (" + relative(lastSeen, now) + ")"}},
		Action: &notify.Action{Label: backupStatusLabel, URL: statusURL},
	}
}
