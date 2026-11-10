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
	if cfg.TopPlaylists.Periods != "week,month,year" || cfg.TopPlaylists.TimeZone != "UTC" ||
		cfg.TopPlaylists.Live != "week" || cfg.TopPlaylists.LiveRefresh != "1h" ||
		cfg.TopPlaylists.Keep != "3" || cfg.TopPlaylists.Verify != "1h" || cfg.TopPlaylists.Owner != "" {
		t.Errorf("defaults = %+v", cfg.TopPlaylists)
	}
	set()
	t.Setenv("TOP_PLAYLISTS_PERIODS", "none")
	t.Setenv("TOP_PLAYLISTS_TIME_ZONE", "America/New_York")
	if cfg, err = Load[API](); err != nil || cfg.TopPlaylists.Periods != "none" || cfg.TopPlaylists.TimeZone != "America/New_York" {
		t.Errorf("got %+v, %v", cfg.TopPlaylists, err)
	}
	set()
	t.Setenv("TOP_PLAYLISTS_LIVE", "none")
	t.Setenv("TOP_PLAYLISTS_LIVE_REFRESH", "not-a-duration") // parsed (and rejected) later, never fatal
	if cfg, err = Load[API](); err != nil || cfg.TopPlaylists.Live != "none" || cfg.TopPlaylists.LiveRefresh != "not-a-duration" {
		t.Errorf("got %+v, %v", cfg.TopPlaylists, err)
	}
	set()
	t.Setenv("TOP_PLAYLISTS_KEEP", "-1") // parsed (and rejected) later, never fatal
	t.Setenv("TOP_PLAYLISTS_VERIFY", "off")
	t.Setenv("TOP_PLAYLISTS_OWNER", "alex")
	if cfg, err = Load[API](); err != nil || cfg.TopPlaylists.Keep != "-1" || cfg.TopPlaylists.Verify != "off" ||
		cfg.TopPlaylists.Owner != "alex" {
		t.Errorf("got %+v, %v", cfg.TopPlaylists, err)
	}
}

func TestLoadAPIBrand(t *testing.T) {
	set := func() {
		t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
		t.Setenv("REDIS_URL", "redis://localhost:6379/0")
		t.Setenv("SECRETS_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
		t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")
	}
	// t.Setenv restores the original value after the test; Unsetenv then
	// removes it for this test only.
	t.Setenv("BRAND_API_URL", "")
	if err := os.Unsetenv("BRAND_API_URL"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BRAND_REFRESH", "")
	if err := os.Unsetenv("BRAND_REFRESH"); err != nil {
		t.Fatal(err)
	}
	set()
	cfg, err := Load[API]()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Brand.APIURL != DefaultBrandAPIURL || cfg.Brand.Refresh != 60*time.Second {
		t.Errorf("defaults = %+v", cfg.Brand)
	}

	set()
	t.Setenv("BRAND_API_URL", "")
	if cfg, err = Load[API](); err != nil || cfg.Brand.APIURL != "" {
		t.Errorf("empty BRAND_API_URL = %+v, %v; want disabled", cfg.Brand, err)
	}

	set()
	t.Setenv("BRAND_API_URL", "  https://staging.baoopn.com/api/v1/brand/listening ")
	t.Setenv("BRAND_REFRESH", "5m")
	if cfg, err = Load[API](); err != nil || cfg.Brand.APIURL != "https://staging.baoopn.com/api/v1/brand/listening" ||
		cfg.Brand.Refresh != 5*time.Minute {
		t.Errorf("custom = %+v, %v", cfg.Brand, err)
	}

	for _, bad := range []string{"5s", "25h"} {
		set()
		t.Setenv("BRAND_REFRESH", bad)
		if _, err := Load[API](); err == nil {
			t.Errorf("BRAND_REFRESH=%s: expected an error", bad)
		}
	}
}
