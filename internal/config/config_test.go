package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/lrclib"
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

	for _, off := range []string{"off", "OFF", " none ", "disabled"} {
		set()
		t.Setenv("BRAND_API_URL", off)
		if cfg, err = Load[API](); err != nil || cfg.Brand.APIURL != "" {
			t.Errorf("BRAND_API_URL=%q = %+v, %v; want disabled", off, cfg.Brand, err)
		}
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

func TestLoadAPIMCP(t *testing.T) {
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
	if !cfg.MCP.Enabled || cfg.MCP.AdminToken != "" || cfg.MCP.RateLimitPerMinute != 60 {
		t.Errorf("defaults = %+v", cfg.MCP)
	}

	set()
	t.Setenv("MCP_ADMIN_TOKEN", "too-short")
	if _, err := Load[API](); err == nil {
		t.Error("expected an error for a short MCP_ADMIN_TOKEN")
	}

	set()
	t.Setenv("MCP_ADMIN_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("MCP_ENABLED", "false")
	t.Setenv("MCP_RATE_LIMIT_PER_MINUTE", "10")
	if cfg, err = Load[API](); err != nil || cfg.MCP.Enabled || cfg.MCP.RateLimitPerMinute != 10 ||
		cfg.MCP.AdminToken != "0123456789abcdef0123456789abcdef" {
		t.Errorf("custom: enabled=%v rate=%d token_set=%v err=%v",
			cfg.MCP.Enabled, cfg.MCP.RateLimitPerMinute, cfg.MCP.AdminToken != "", err)
	}
	if _, ok := os.LookupEnv("MCP_ADMIN_TOKEN"); ok {
		t.Error("MCP_ADMIN_TOKEN should be removed from the environment after load")
	}

	set()
	t.Setenv("MCP_RATE_LIMIT_PER_MINUTE", "-1")
	if _, err := Load[API](); err == nil {
		t.Error("expected an error for a negative MCP_RATE_LIMIT_PER_MINUTE")
	}
}

func TestLoadAPILyrics(t *testing.T) {
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
	l := cfg.Lyrics
	if !l.Enabled || l.APIURL != "https://lrclib.net" || l.Interval != time.Minute || l.Batch != 10 ||
		l.Spacing != 2*time.Second || l.NotFoundRetry != 720*time.Hour || l.Lease != 20*time.Minute {
		t.Errorf("defaults = %+v", l)
	}

	set()
	t.Setenv("LYRICS_API_URL", " http://lrclib.local:3000/ ")
	if cfg, err = Load[API](); err != nil || cfg.Lyrics.APIURL != "http://lrclib.local:3000" {
		t.Errorf("trimmed URL = %q, %v", cfg.Lyrics.APIURL, err)
	}

	for k, v := range map[string]string{
		"LYRICS_API_URL": "lrclib.net", "LYRICS_INTERVAL": "5s", "LYRICS_BATCH": "101",
		"LYRICS_SPACING": "500ms", "LYRICS_NOT_FOUND_RETRY": "1h", "LYRICS_LEASE": "1m",
	} {
		set()
		t.Setenv(k, v)
		if _, err := Load[API](); err == nil {
			t.Errorf("%s=%s: expected an error", k, v)
		}
		// Disabled skips validation.
		set()
		t.Setenv(k, v)
		t.Setenv("LYRICS_ENABLED", "false")
		if _, err := Load[API](); err != nil {
			t.Errorf("%s=%s with LYRICS_ENABLED=false: %v", k, v, err)
		}
		t.Setenv("LYRICS_ENABLED", "true")
	}

	// The lease must cover one batch at 5 requests a track, each with the
	// spacing and the request timeout (5 × 20 × (10s + 20s) = 50m).
	long := func(lease string) error {
		set()
		for k, v := range map[string]string{"LYRICS_API_URL": "https://lrclib.net", "LYRICS_INTERVAL": "1m",
			"LYRICS_NOT_FOUND_RETRY": "720h", "LYRICS_BATCH": "20", "LYRICS_SPACING": "10s", "LYRICS_LEASE": lease} {
			t.Setenv(k, v)
		}
		_, err := Load[API]()
		return err
	}
	if err := long("49m"); err == nil || !strings.Contains(err.Error(), "LYRICS_LEASE") {
		t.Errorf("short lease = %v, want a LYRICS_LEASE error", err)
	}
	if err := long("50m"); err != nil {
		t.Errorf("50m lease: %v", err)
	}
}

func TestLoadAPITranslate(t *testing.T) {
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
	tr := cfg.Translate
	if tr.Enabled || tr.Model != "claude-sonnet-5-5" || tr.Effort != "low" || tr.Interval != time.Minute ||
		tr.Batch != 5 || tr.Spacing != 15*time.Second || tr.Lease != 10*time.Minute ||
		tr.RecentWindow != 30*time.Minute || tr.DailyCap != 100 || tr.MonthlyCap != 1000 || tr.MaxAttempts != 5 {
		t.Errorf("defaults = %+v", tr)
	}

	// Enabled without a key fails, and the message never holds a value.
	set()
	t.Setenv("TRANSLATE_ENABLED", "true")
	t.Setenv("ANTHROPIC_API_KEY", " ")
	if _, err := Load[API](); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY is required") {
		t.Errorf("no key: %v", err)
	}

	set()
	t.Setenv("TRANSLATE_ENABLED", "true")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-not-a-real-key")
	if cfg, err = Load[API](); err != nil || cfg.Translate.APIKey != "sk-test-not-a-real-key" {
		t.Fatalf("enabled: %v", err)
	}
	if _, ok := os.LookupEnv("ANTHROPIC_API_KEY"); ok {
		t.Error("ANTHROPIC_API_KEY should be removed from the environment after load")
	}

	for k, v := range map[string]string{
		"TRANSLATE_MODEL": "Claude Opus", "TRANSLATE_EFFORT": "max", "TRANSLATE_INTERVAL": "1s",
		"TRANSLATE_BATCH": "51", "TRANSLATE_SPACING": "0s", "TRANSLATE_LEASE": "1m",
		"TRANSLATE_RECENT_WINDOW": "48h", "TRANSLATE_DAILY_CAP": "-1", "TRANSLATE_MONTHLY_CAP": "100001",
		"TRANSLATE_MAX_ATTEMPTS": "0",
	} {
		set()
		t.Setenv("TRANSLATE_ENABLED", "true")
		t.Setenv("ANTHROPIC_API_KEY", "sk-test-not-a-real-key")
		t.Setenv(k, v)
		if _, err := Load[API](); err == nil {
			t.Errorf("%s=%s: expected an error", k, v)
		} else if strings.Contains(err.Error(), "sk-test") {
			t.Errorf("%s: error leaks the key", k)
		}
	}
	set()
	t.Setenv("TRANSLATE_ENABLED", "true")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-not-a-real-key")
	t.Setenv("TRANSLATE_DAILY_CAP", "0")
	t.Setenv("TRANSLATE_MONTHLY_CAP", "0")
	if _, err := Load[API](); err == nil {
		t.Error("both caps 0: expected an error")
	}
}

// The LYRICS_LEASE check uses the LRCLIB client's request budget and
// timeout; the defaults pass it.
func TestLyricsLeaseMatchesClient(t *testing.T) {
	if lyricsRequestsPerTrack != lrclib.MaxRequestsPerLookup {
		t.Errorf("lyricsRequestsPerTrack = %d, lrclib.MaxRequestsPerLookup = %d", lyricsRequestsPerTrack, lrclib.MaxRequestsPerLookup)
	}
	if lyricsRequestTimeout != lrclib.DefaultTimeout {
		t.Errorf("lyricsRequestTimeout = %s, lrclib.DefaultTimeout = %s", lyricsRequestTimeout, lrclib.DefaultTimeout)
	}
	l := Lyrics{Enabled: true, APIURL: "https://lrclib.net", Interval: time.Minute, Batch: 10, Spacing: 2 * time.Second,
		NotFoundRetry: 720 * time.Hour, Lease: 20 * time.Minute}
	if err := l.validate(); err != nil {
		t.Errorf("defaults: %v", err)
	}
	l.Lease = 18 * time.Minute // 5 × 10 × 22s = 18m20s
	if err := l.validate(); err == nil {
		t.Error("18m lease with the default batch passed")
	}
	l.Lease = 15 * time.Minute // the old default
	if err := l.validate(); err == nil {
		t.Error("15m lease with the default batch passed")
	}
}
