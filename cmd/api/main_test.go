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
