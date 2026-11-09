package alerts

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/notify"
)

func TestExpiryEmail(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	const reauth = "https://listening.example.com/admin/settings/spotify"
	tests := []struct {
		stage   int
		left    time.Duration
		tone    notify.Tone
		badge   string
		subject string
		rel     string
	}{
		{7, 7 * 24 * time.Hour, notify.Warning, "Expires in 7 days", "[spotify-tracker] Spotify token expires in 7 days", "(in 7 days)"},
		{1, 5 * time.Hour, notify.Warning, "Expires in 1 day", "[spotify-tracker] Spotify token expires in 1 day", "(in 5 hours)"},
		{0, -3 * 24 * time.Hour, notify.Danger, "Expired", "[spotify-tracker] Spotify token has expired", "(3 days ago)"},
	}
	for _, tt := range tests {
		e := ExpiryEmail(tt.stage, now.Add(tt.left), 180, reauth, now)
		if e.Tone != tt.tone || e.Badge != tt.badge || e.Subject != tt.subject {
			t.Errorf("stage %d: tone %q badge %q subject %q", tt.stage, e.Tone, e.Badge, e.Subject)
		}
		if e.Action == nil || e.Action.URL != reauth || e.Action.Label != "Re-authorize Spotify" {
			t.Errorf("stage %d: action %+v", tt.stage, e.Action)
		}
		if len(e.Facts) != 2 || !strings.HasSuffix(e.Facts[0].Value, tt.rel) || e.Facts[1].Value != "180-day limit" {
			t.Errorf("stage %d: facts %+v", tt.stage, e.Facts)
		}
	}
	if e := ExpiryEmail(0, now, 180, reauth, now); !strings.Contains(e.Paragraphs[0], "not being recorded") {
		t.Error("expired email does not say tracking stopped")
	}
}

func TestInvalidGrantEmail(t *testing.T) {
	e := InvalidGrantEmail(errors.New(`oauth2: "invalid_grant" <b>`), "https://listening.example.com/admin/settings/spotify")
	if e.Tone != notify.Danger || e.Badge != "Action needed" || e.Title != "Spotify rejected the saved login" {
		t.Errorf("got %+v", e)
	}
	if e.Code != `oauth2: "invalid_grant" <b>` || e.Action == nil {
		t.Errorf("code %q action %+v", e.Code, e.Action)
	}
	if InvalidGrantEmail(nil, "").Code == "" {
		t.Error("nil cause")
	}
}

func TestRelative(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		30 * time.Minute:     "in 1 hour",
		25 * time.Hour:       "in 1 day",
		-2 * time.Hour:       "less than a day ago",
		-49 * time.Hour:      "2 days ago",
		180 * 24 * time.Hour: "in 180 days",
	} {
		if got := relative(now.Add(d), now); got != want {
			t.Errorf("relative(%s) = %q, want %q", d, got, want)
		}
	}
}
