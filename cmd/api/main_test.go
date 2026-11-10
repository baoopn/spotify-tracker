package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/staticsite"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

// The public page may frame only the Spotify embed player.
func TestPublicSiteCSPAllowsSpotifyEmbed(t *testing.T) {
	csp := staticsite.BuildCSP(nil, publicSiteOptions().ExtraCSP)
	if !strings.Contains(csp, "frame-src https://open.spotify.com;") {
		t.Errorf("frame-src should allow only open.spotify.com: %s", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("frame-ancestors must stay 'none': %s", csp)
	}
}

// The live footer credit: fetched from baoopn.com, logo from media.baoopn.com.
func TestPublicSiteCSPAllowsSiteCredit(t *testing.T) {
	csp := staticsite.BuildCSP(nil, publicSiteOptions().ExtraCSP)
	directives := map[string]string{}
	for _, d := range strings.Split(csp, "; ") {
		name, value, _ := strings.Cut(d, " ")
		directives[name] = value
	}
	if got := directives["connect-src"]; got != "'self' https://baoopn.com" {
		t.Errorf("connect-src = %q, want 'self' https://baoopn.com", got)
	}
	if !strings.Contains(" "+directives["img-src"]+" ", " https://media.baoopn.com ") {
		t.Errorf("img-src should allow media.baoopn.com: %q", directives["img-src"])
	}
	if got := directives["frame-src"]; got != "https://open.spotify.com" {
		t.Errorf("frame-src = %q, want only https://open.spotify.com", got)
	}
	if strings.Contains(directives["script-src"], "'unsafe-inline'") {
		t.Errorf("script-src must not allow 'unsafe-inline': %q", directives["script-src"])
	}
}

func TestTopPlaylistsLiveConfig(t *testing.T) {
	parse := func(periods, live, refresh string) (topplaylists.Config, string) {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		cfg := config.API{TopPlaylists: config.TopPlaylists{Periods: periods, TimeZone: "UTC", Live: live, LiveRefresh: refresh}}
		return topPlaylistsConfig(cfg, log), buf.String()
	}

	cfg, logs := parse("week,month,year", "week", "1h")
	if !cfg.IsLive(topplaylists.Week) || cfg.IsLive(topplaylists.Month) || cfg.Refresh() != time.Hour ||
		strings.Contains(logs, "level=WARN") || strings.Contains(logs, "level=ERROR") {
		t.Errorf("defaults: %+v\n%s", cfg, logs)
	}
	if cfg, _ = parse("week", "none", "1h"); len(cfg.Live) != 0 {
		t.Errorf("none: %+v", cfg.Live)
	}
	cfg, logs = parse("month", "week,month", "2h")
	if len(cfg.Live) != 1 || cfg.Live[0] != topplaylists.Month || cfg.Refresh() != 2*time.Hour ||
		!strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "ignored=week") {
		t.Errorf("subset: %+v\n%s", cfg, logs)
	}
	cfg, logs = parse("week", "weekly", "5m")
	if !cfg.Enabled() || len(cfg.Live) != 0 || cfg.Refresh() != time.Hour || strings.Count(logs, "level=ERROR") != 2 {
		t.Errorf("invalid: %+v\n%s", cfg, logs)
	}
}

func TestTopPlaylistsKeepConfig(t *testing.T) {
	parse := func(keep, verify, owner string) (topplaylists.Config, string) {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		cfg := config.API{TopPlaylists: config.TopPlaylists{Periods: "week", TimeZone: "UTC", Live: "week",
			LiveRefresh: "1h", Keep: keep, Verify: verify, Owner: owner}}
		return topPlaylistsConfig(cfg, log), buf.String()
	}
	cfg, logs := parse("3", "1h", "")
	if cfg.Keep != 3 || cfg.Verify != time.Hour || cfg.Owner != "" || strings.Contains(logs, "level=ERROR") {
		t.Errorf("defaults: %+v\n%s", cfg, logs)
	}
	cfg, logs = parse("0", "off", "  alex ")
	if cfg.Keep != 0 || cfg.Verify != 0 || cfg.Owner != "alex" || strings.Contains(logs, "level=ERROR") {
		t.Errorf("off: %+v\n%s", cfg, logs)
	}
	cfg, logs = parse("-2", "5m", strings.Repeat("a", 41))
	if cfg.Keep != 3 || cfg.Verify != time.Hour || len(cfg.Owner) != 40 || strings.Count(logs, "level=ERROR") != 3 {
		t.Errorf("invalid: %+v\n%s", cfg, logs)
	}
	if strings.Contains(logs, strings.Repeat("a", 41)) {
		t.Error("full owner name logged")
	}
}
