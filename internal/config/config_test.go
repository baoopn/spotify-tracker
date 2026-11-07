package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadPollerDefaults(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")

	cfg, err := Load[Poller]()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval() != 3*time.Second {
		t.Errorf("default poll interval = %s, want 3s", cfg.PollInterval())
	}
	if cfg.TrackerGRPCAddr != "api:9000" || cfg.OpsAddr != ":9090" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestSecretsAreUnsetAfterLoad(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")
	if _, err := Load[Poller](); err != nil {
		t.Fatal(err)
	}
	if _, ok := os.LookupEnv("INTERNAL_API_TOKEN"); ok {
		t.Error("INTERNAL_API_TOKEN should be removed from the environment after load")
	}
}

func TestLoadPollerRejectsBadInterval(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")
	t.Setenv("POLL_INTERVAL_MS", "10")
	if _, err := Load[Poller](); err == nil {
		t.Fatal("expected error for tiny interval")
	}
}

func TestLoadAPIRequiresSecrets(t *testing.T) {
	// Secrets are tagged `unset`, so they are removed from the environment
	// after each load; set them again before every attempt.
	base := func() {
		t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
		t.Setenv("REDIS_URL", "redis://localhost:6379/0")
		t.Setenv("SECRETS_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	}
	base()
	if _, err := Load[API](); err == nil {
		t.Fatal("expected error when INTERNAL_API_TOKEN is missing")
	}
	base()
	t.Setenv("INTERNAL_API_TOKEN", "short")
	if _, err := Load[API](); err == nil {
		t.Fatal("expected error for short token")
	}
	base()
	t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://a.example,https://b.example")
	cfg, err := Load[API]()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CacheTTL != 5*time.Minute {
		t.Errorf("unexpected: %+v", cfg)
	}
	if cfg.Spotify.RedirectURI != "http://127.0.0.1:8080/admin/api/spotify/callback" {
		t.Errorf("redirect uri default = %q", cfg.Spotify.RedirectURI)
	}
}

func TestLoadAPITopPlaylists(t *testing.T) {
	set := func() {
		t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
		t.Setenv("REDIS_URL", "redis://localhost:6379/0")
		t.Setenv("SECRETS_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
		t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")
	}
	set()
	cfg, err := Load[API]()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TopPlaylists.Periods != "week,month,year" || cfg.TopPlaylists.TimeZone != "UTC" {
		t.Errorf("defaults = %+v", cfg.TopPlaylists)
	}
	set()
	t.Setenv("TOP_PLAYLISTS_PERIODS", "none")
	t.Setenv("TOP_PLAYLISTS_TIME_ZONE", "America/New_York")
	if cfg, err = Load[API](); err != nil || cfg.TopPlaylists.Periods != "none" || cfg.TopPlaylists.TimeZone != "America/New_York" {
		t.Errorf("got %+v, %v", cfg.TopPlaylists, err)
	}
}
