package alerts

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/notify"
)

const reauthLabel = "Re-authorize Spotify"

// days is "1 day" or "N days".
func days(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// relative says how far t is from now in whole days ("in 7 days",
// "2 days ago"), or in hours when it is less than a day away.
func relative(t, now time.Time) string {
	d := t.Sub(now)
	switch {
	case d >= 24*time.Hour:
		return "in " + days(int(math.Round(d.Hours()/24)))
	case d > 0:
		h := max(int(math.Ceil(d.Hours())), 1)
		if h == 1 {
			return "in 1 hour"
		}
		return fmt.Sprintf("in %d hours", h)
	case d > -24*time.Hour:
		return "less than a day ago"
	}
	return days(int(math.Round(-d.Hours()/24))) + " ago"
}

// ExpiryEmail is the alert for an expiry stage (stage 0 = expired).
func ExpiryEmail(stage int, expires time.Time, lifetimeDays int, reauthURL string, now time.Time) notify.Email {
	// Always state the zone: recipients may live anywhere.
	at := expires.UTC().Format("2006-01-02 15:04 UTC")
	facts := []notify.Fact{
		{Label: "Expires", Value: at + " (" + relative(expires, now) + ")"},
		{Label: "Token lifetime", Value: fmt.Sprintf("%d-day limit", lifetimeDays)},
	}
	action := &notify.Action{Label: reauthLabel, URL: reauthURL}
	note := "The poller picks up the new token automatically."
	if stage == 0 {
		facts[0].Label = "Expired"
		return notify.Email{
			Subject:   "[spotify-tracker] Spotify token has expired",
			Preheader: "Listening history is not being recorded. Re-authorize Spotify to resume.",
			Tone:      notify.Danger,
			Badge:     "Expired",
			Title:     "Your Spotify connection has expired",
			Paragraphs: []string{
				"Listening history is not being recorded. The Spotify login that spotify-tracker uses has run out.",
				"Re-authorize Spotify in the admin dashboard to start tracking again.",
			},
			Facts: facts, Action: action, Note: note,
		}
	}
	return notify.Email{
		Subject:   "[spotify-tracker] Spotify token expires in " + days(stage),
		Preheader: "Re-authorize Spotify before " + at + " to keep tracking.",
		Tone:      notify.Warning,
		Badge:     "Expires in " + days(stage),
		Title:     "Your Spotify connection expires soon",
		Paragraphs: []string{
			"Spotify logins only last a fixed time. When this one runs out, spotify-tracker stops recording your listening history.",
			"Re-authorize Spotify in the admin dashboard before then. It takes a few seconds.",
		},
		Facts: facts, Action: action, Note: note,
	}
}

// InvalidGrantEmail is the alert for Spotify rejecting the saved refresh
// token.
func InvalidGrantEmail(cause error, reauthURL string) notify.Email {
	msg := "unknown error"
	if cause != nil {
		msg = cause.Error()
	}
	return notify.Email{
		Subject:   "[spotify-tracker] Spotify rejected the saved login",
		Preheader: "Listening history is not being recorded until you re-authorize Spotify.",
		Tone:      notify.Danger,
		Badge:     "Action needed",
		Title:     "Spotify rejected the saved login",
		Paragraphs: []string{
			"Spotify refused the refresh token that spotify-tracker uses. Listening history is not being recorded until this is fixed.",
			"This happens when the token expires or access is revoked. Re-authorize Spotify to fix it. Spotify said:",
		},
		Code:   msg,
		Action: &notify.Action{Label: reauthLabel, URL: reauthURL},
		Note:   "The poller picks up the new token automatically.",
	}
}

// Sample is a named example email for previews and golden tests.
type Sample struct {
	Name  string
	Email notify.Email
}

// Samples returns one of each alert email, as of now, for a site at
// publicURL.
func Samples(publicURL string, now time.Time) []Sample {
	b, _ := notify.NewBrand(publicURL, "")
	reauth := b.URL + "/admin/settings/spotify"
	exp := now.Add(7*24*time.Hour - 3*time.Hour)
	status := b.URL + BackupStatusPath
	ok, failed := sampleBackupRuns(now)
	at := "03:00"
	return []Sample{
		{"expiry-warning", ExpiryEmail(7, exp, 180, reauth, now)},
		{"expired", ExpiryEmail(0, now.Add(-26*time.Hour), 180, reauth, now)},
		{"invalid-grant", InvalidGrantEmail(errors.New("spotify: refresh token: oauth2: \"invalid_grant\" \"Refresh token revoked\""), reauth)},
		{"test", notify.TestEmail("admin@example.com", "smtp.example.com", b, now)},
		{"backup-failing", BackupFailingEmail(failed, ok, status, now)},
		{"backup-stale", BackupStaleEmail(ok, &domain.BackupAgent{BackupTime: &at}, status, now.Add(20*time.Hour))},
		{"backup-offline", BackupOfflineEmail(now.Add(-90*time.Minute), status, now)},
	}
}

// sampleBackupRuns are a successful run a day before now and a failed one
// nine hours before now.
func sampleBackupRuns(now time.Time) (ok, failed *domain.BackupRun) {
	okEnd := now.Add(-33*time.Hour + 2*time.Minute)
	name := "spotify-tracker-20261108T030000Z.dump.age"
	failEnd := now.Add(-9*time.Hour + time.Minute)
	msg := "upload failed: rclone: 403 Forbidden (check the bucket token)"
	return &domain.BackupRun{ID: 41, Trigger: "schedule", Status: domain.BackupSuccess,
			StartedAt: now.Add(-33 * time.Hour), FinishedAt: &okEnd, Name: &name},
		&domain.BackupRun{ID: 42, Trigger: "schedule", Status: domain.BackupFailed,
			StartedAt: now.Add(-9 * time.Hour), FinishedAt: &failEnd, Error: &msg}
}
