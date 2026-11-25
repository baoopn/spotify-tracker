// Package config loads service configuration from environment variables.
//
// Each binary loads only the struct it needs, so a worker never requires
// secrets that belong to another service.
package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Common settings shared by every service.
type Common struct {
	Env       string `env:"APP_ENV" envDefault:"production"`
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`
	// OpsAddr serves /healthz, /readyz and /metrics. Never expose publicly.
	OpsAddr string `env:"OPS_ADDR" envDefault:":9090"`
}

// IsDevelopment reports whether the service runs in development mode.
func (c Common) IsDevelopment() bool { return c.Env == "development" }

// Postgres connection settings.
type Postgres struct {
	URL             string        `env:"DATABASE_URL,required,unset"`
	MaxConns        int32         `env:"DATABASE_MAX_CONNS" envDefault:"10"`
	MinConns        int32         `env:"DATABASE_MIN_CONNS" envDefault:"1"`
	MaxConnLifetime time.Duration `env:"DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
}

// Redis connection settings.
type Redis struct {
	URL string `env:"REDIS_URL,required,unset"`
}

// API is the configuration of cmd/api.
type API struct {
	Common
	Postgres
	Redis

	HTTPAddr           string        `env:"HTTP_ADDR" envDefault:":8080"`
	GRPCAddr           string        `env:"GRPC_ADDR" envDefault:":9000"`
	InternalToken      string        `env:"INTERNAL_API_TOKEN,required,unset"`
	CORSAllowedOrigins []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:","`
	CacheTTL           time.Duration `env:"CACHE_TTL" envDefault:"5m"`
	// ShowPrivatePlaylists lists private playlists on the public page too.
	ShowPrivatePlaylists bool `env:"PUBLIC_SHOW_PRIVATE_PLAYLISTS" envDefault:"false"`
	RateLimitPerMinute   int  `env:"RATE_LIMIT_PER_MINUTE" envDefault:"240"`
	MigrateOnStart       bool `env:"MIGRATE_ON_START" envDefault:"true"`
	// TrustedProxyHops is how many reverse proxies append to X-Forwarded-For
	// in front of the API (1 = Traefik on Coolify or Caddy on a VPS; 2 when
	// Cloudflare proxies in front of those). 0 uses the TCP peer address.
	TrustedProxyHops int `env:"TRUSTED_PROXY_HOPS" envDefault:"1"`
	// ClientIPHeader, when set (e.g. "CF-Connecting-IP"), takes the client IP
	// from that header instead of X-Forwarded-For.
	ClientIPHeader string `env:"CLIENT_IP_HEADER"`

	// PublicURL is the externally visible base URL of the API, e.g.
	// https://spotify-api.example.com (used for OAuth redirects).
	PublicURL string `env:"PUBLIC_URL" envDefault:"http://127.0.0.1:8080"`
	// SecretsKey encrypts tokens/passwords stored in the database
	// (32 bytes, hex or base64). Required.
	SecretsKey string `env:"SECRETS_KEY,required,unset"`

	Spotify
	Email
	Brand
	Admin
	TopPlaylists
	MCP
	Lyrics
	Translate
	Prefetch

	EnrichMaxAttempts int `env:"ENRICH_MAX_ATTEMPTS" envDefault:"8"`
	// ReplayResetThreshold is how far the progress must jump back on the
	// same track to count as a replay.
	ReplayResetThreshold time.Duration `env:"REPLAY_RESET_THRESHOLD" envDefault:"10s"`
	// PlayCountMinFraction is the share of a track that must be heard before
	// a listen counts as a play (30 s when the duration is unknown).
	PlayCountMinFraction float64 `env:"PLAY_COUNT_MIN_FRACTION" envDefault:"0.3"`
	// PlayResumeWindow is how long a paused or idle song can resume as the
	// same listen instead of counting another play.
	PlayResumeWindow time.Duration `env:"PLAY_RESUME_WINDOW" envDefault:"24h"`
}

// Poller is the configuration of cmd/poller. It holds no Spotify secrets:
// access tokens are obtained from the API over gRPC.
type Poller struct {
	Common
	Redis

	TrackerGRPCAddr string `env:"TRACKER_GRPC_ADDR" envDefault:"api:9000"`
	InternalToken   string `env:"INTERNAL_API_TOKEN,required,unset"`
	PollIntervalMS  int    `env:"POLL_INTERVAL_MS" envDefault:"3000"`
	QueuePrefetch
}

// Email holds SMTP seed values for alert emails. They are copied into the
// database on first start (and whenever they change) and are editable in the
// admin dashboard afterwards. Names match the legacy Directus deployment.
type Email struct {
	SMTPHost     string `env:"EMAIL_SMTP_HOST"`
	SMTPPort     int    `env:"EMAIL_SMTP_PORT" envDefault:"587"`
	SMTPUser     string `env:"EMAIL_SMTP_USER"`
	SMTPPassword string `env:"EMAIL_SMTP_PASSWORD,unset"`
	From         string `env:"EMAIL_FROM"`
	// AlertTo is a comma-separated list of alert recipients.
	AlertTo string `env:"ALERT_EMAIL_TO"`
	// LogoURL overrides the absolute http(s) URL of the wordmark PNG shown in
	// alert emails (default: PUBLIC_URL + /email/listening-wordmark-primary@2x.png).
	LogoURL string `env:"EMAIL_LOGO_URL"`
}

// DefaultBrandAPIURL is the baoopn brand API the listening brand files come
// from when BRAND_API_URL is not set.
const DefaultBrandAPIURL = "https://baoopn.com/api/v1/brand/listening"

// Brand configures the brand files (logos, icons, favicons, email logo)
// read from the baoopn brand API, with the repository's static copies as
// the fallback. The URL itself is checked by internal/brandkit, so a bad
// value falls back to the static copies instead of stopping the API.
type Brand struct {
	// APIURL is the brand API endpoint. Unset uses DefaultBrandAPIURL; an
	// empty value disables it (the static copies are always used).
	APIURL string `env:"BRAND_API_URL"`
	// Refresh is the time between two fetches of the brand API (10s..24h).
	Refresh time.Duration `env:"BRAND_REFRESH" envDefault:"60s"`
}

// Spotify holds the Spotify app credentials used by the API.
type Spotify struct {
	ClientID     string `env:"SPOTIFY_CLIENT_ID"`
	ClientSecret string `env:"SPOTIFY_CLIENT_SECRET,unset"`
	// RefreshToken / AuthorizedAt seed the stored token (optional once the
	// app has been authorized from the dashboard).
	RefreshToken string `env:"SPOTIFY_REFRESH_TOKEN,unset"`
	AuthorizedAt string `env:"SPOTIFY_AUTHORIZED_AT"`
	// TokenLifetimeDays is the refresh-token lifetime shown in the Spotify
	// dashboard (180 days for Development Mode apps).
	TokenLifetimeDays int `env:"SPOTIFY_TOKEN_LIFETIME_DAYS" envDefault:"180"`
	// RedirectURI defaults to PUBLIC_URL + /admin/api/spotify/callback. It
	// must be registered in the Spotify Developer Dashboard.
	RedirectURI string `env:"SPOTIFY_REDIRECT_URI"`
	// OwnerID, when set, is the only Spotify user ID that may be connected
	// (prevents binding someone else's account by mistake).
	OwnerID string `env:"SPOTIFY_OWNER_ID"`
}

// TopPlaylists configures the auto-generated "Top 50" Spotify playlists.
// Values are parsed (and invalid ones reported) by internal/topplaylists, so
// a typo disables the feature instead of stopping the API.
type TopPlaylists struct {
	// Periods is a comma-separated list of week, month, year; "none"
	// disables the feature (an empty value falls back to the default).
	Periods string `env:"TOP_PLAYLISTS_PERIODS" envDefault:"week,month,year"`
	// TimeZone is the IANA zone that defines period boundaries and labels.
	TimeZone string `env:"TOP_PLAYLISTS_TIME_ZONE" envDefault:"UTC"`
	// Live lists the kinds whose current period gets a live playlist,
	// refreshed while listening and finalized when the period ends; "none"
	// disables live playlists.
	Live string `env:"TOP_PLAYLISTS_LIVE" envDefault:"week"`
	// LiveRefresh is the time between two refreshes of a live playlist (Go
	// duration, at least 10m; kept as text so a typo falls back to 1h).
	LiveRefresh string `env:"TOP_PLAYLISTS_LIVE_REFRESH" envDefault:"1h"`
	// Keep is how many playlists of each kind are kept (the live one
	// counts); older ones are removed from Spotify. "0" keeps all. Kept as
	// text so a typo falls back to 3.
	Keep string `env:"TOP_PLAYLISTS_KEEP" envDefault:"3"`
	// Verify is how often the published playlists are checked against
	// Spotify and restored when deleted (Go duration, at least 10m; "0" or
	// "off" disables it). Kept as text so a typo falls back to 1h.
	Verify string `env:"TOP_PLAYLISTS_VERIFY" envDefault:"1h"`
	// Owner names the playlists "{Owner}'s Top 50 · …" (at most 40
	// characters). Empty uses the connected Spotify account's display name.
	Owner string `env:"TOP_PLAYLISTS_OWNER"`
}

// MCP configures the remote MCP endpoint (/mcp) served by cmd/api.
type MCP struct {
	// Enabled mounts /mcp; false answers 404 there.
	Enabled bool `env:"MCP_ENABLED" envDefault:"true"`
	// AdminToken, when set (at least 32 characters), is a bootstrap admin
	// token: clients sending "Authorization: Bearer <token>" get the
	// read-only admin tools, like with the token generated in the dashboard.
	AdminToken string `env:"MCP_ADMIN_TOKEN,unset"`
	// RateLimitPerMinute limits /mcp requests per client IP (0 = off).
	RateLimitPerMinute int `env:"MCP_RATE_LIMIT_PER_MINUTE" envDefault:"60"`
}

// Lyrics configures the LRCLIB lyrics fetcher in cmd/api (lyrics are shown
// in the admin dashboard and admin MCP tools only).
type Lyrics struct {
	// Enabled runs the background fetcher; false makes no LRCLIB calls.
	Enabled bool `env:"LYRICS_ENABLED" envDefault:"true"`
	// APIURL is the LRCLIB base URL.
	APIURL string `env:"LYRICS_API_URL" envDefault:"https://lrclib.net"`
	// Interval is the pause between batches.
	Interval time.Duration `env:"LYRICS_INTERVAL" envDefault:"1m"`
	// Batch is how many tracks one batch looks up.
	Batch int `env:"LYRICS_BATCH" envDefault:"10"`
	// Spacing is the minimum time between two LRCLIB requests.
	Spacing time.Duration `env:"LYRICS_SPACING" envDefault:"2s"`
	// NotFoundRetry is when a track with no lyrics found is checked again.
	NotFoundRetry time.Duration `env:"LYRICS_NOT_FOUND_RETRY" envDefault:"720h"`
}

func (l *Lyrics) validate() error {
	if !l.Enabled {
		return nil
	}
	l.APIURL = strings.TrimRight(strings.TrimSpace(l.APIURL), "/")
	u, err := url.Parse(l.APIURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("LYRICS_API_URL must be an absolute http(s) URL")
	}
	switch {
	case l.Interval < 10*time.Second || l.Interval > 24*time.Hour:
		return fmt.Errorf("LYRICS_INTERVAL must be between 10s and 24h")
	case l.Batch < 1 || l.Batch > 100:
		return fmt.Errorf("LYRICS_BATCH must be in 1..100")
	case l.Spacing < time.Second || l.Spacing > time.Minute:
		return fmt.Errorf("LYRICS_SPACING must be between 1s and 1m")
	case l.NotFoundRetry < 24*time.Hour || l.NotFoundRetry > 8760*time.Hour:
		return fmt.Errorf("LYRICS_NOT_FOUND_RETRY must be between 24h and 8760h")
	}
	return nil
}

// Translate configures the Claude lyrics translator in cmd/api. Songs are
// queued only when they play (or an admin re-queues them).
type Translate struct {
	// Enabled runs the translator; false makes no Claude calls.
	Enabled bool `env:"TRANSLATE_ENABLED" envDefault:"false"`
	// APIKey is the Anthropic API key (required when enabled).
	APIKey string `env:"ANTHROPIC_API_KEY,unset"`
	Model  string `env:"TRANSLATE_MODEL" envDefault:"claude-opus-5-5"`
	// Effort is the Claude effort level: low or medium.
	Effort   string        `env:"TRANSLATE_EFFORT" envDefault:"low"`
	Interval time.Duration `env:"TRANSLATE_INTERVAL" envDefault:"1m"`
	Batch    int           `env:"TRANSLATE_BATCH" envDefault:"5"`
	// Spacing is the minimum time between two Claude calls.
	Spacing time.Duration `env:"TRANSLATE_SPACING" envDefault:"15s"`
	// Lease is how long a worker holds a song; a crashed worker's claim
	// expires after it.
	Lease time.Duration `env:"TRANSLATE_LEASE" envDefault:"10m"`
	// RecentWindow also queues songs played this recently (0 = only the
	// song playing now).
	RecentWindow time.Duration `env:"TRANSLATE_RECENT_WINDOW" envDefault:"30m"`
	DailyCap     int           `env:"TRANSLATE_DAILY_CAP" envDefault:"100"`
	MonthlyCap   int           `env:"TRANSLATE_MONTHLY_CAP" envDefault:"1000"`
	MaxAttempts  int           `env:"TRANSLATE_MAX_ATTEMPTS" envDefault:"5"`
}

var modelName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,63}$`)

func (t *Translate) validate() error {
	if !t.Enabled {
		return nil
	}
	t.APIKey = strings.TrimSpace(t.APIKey)
	switch {
	case t.APIKey == "":
		return fmt.Errorf("ANTHROPIC_API_KEY is required when TRANSLATE_ENABLED=true")
	case !modelName.MatchString(t.Model):
		return fmt.Errorf("TRANSLATE_MODEL must be a model id such as claude-opus-5-5")
	case t.Effort != "low" && t.Effort != "medium":
		return fmt.Errorf("TRANSLATE_EFFORT must be low or medium")
	case t.Interval < 10*time.Second || t.Interval > 24*time.Hour:
		return fmt.Errorf("TRANSLATE_INTERVAL must be between 10s and 24h")
	case t.Batch < 1 || t.Batch > 50:
		return fmt.Errorf("TRANSLATE_BATCH must be in 1..50")
	case t.Spacing < time.Second || t.Spacing > 10*time.Minute:
		return fmt.Errorf("TRANSLATE_SPACING must be between 1s and 10m")
	case t.Lease < 5*time.Minute || t.Lease > time.Hour:
		return fmt.Errorf("TRANSLATE_LEASE must be between 5m and 1h")
	case t.RecentWindow < 0 || t.RecentWindow > 24*time.Hour:
		return fmt.Errorf("TRANSLATE_RECENT_WINDOW must be between 0 and 24h")
	case t.DailyCap < 0 || t.DailyCap > 10_000:
		return fmt.Errorf("TRANSLATE_DAILY_CAP must be in 0..10000")
	case t.MonthlyCap < 0 || t.MonthlyCap > 100_000:
		return fmt.Errorf("TRANSLATE_MONTHLY_CAP must be in 0..100000")
	case t.DailyCap == 0 && t.MonthlyCap == 0:
		return fmt.Errorf("set TRANSLATE_DAILY_CAP or TRANSLATE_MONTHLY_CAP above 0")
	case t.MaxAttempts < 1 || t.MaxAttempts > 20:
		return fmt.Errorf("TRANSLATE_MAX_ATTEMPTS must be in 1..20")
	}
	return nil
}

// minMCPAdminTokenLen is the shortest accepted MCP_ADMIN_TOKEN.
const minMCPAdminTokenLen = 32

// Admin configures the admin dashboard (Clerk authentication).
type Admin struct {
	ClerkSecretKey      string   `env:"CLERK_SECRET_KEY,unset"`
	ClerkPublishableKey string   `env:"CLERK_PUBLISHABLE_KEY"`
	AllowedEmails       []string `env:"ADMIN_ALLOWED_EMAILS" envSeparator:","`
	AllowedUserIDs      []string `env:"ADMIN_ALLOWED_USER_IDS" envSeparator:","`
	// AdminRole grants access to Clerk users whose public_metadata.role
	// equals it (in addition to the allow-lists). Empty disables role checks.
	AdminRole string `env:"ADMIN_ROLE" envDefault:"admin"`
}

// Enabled reports whether the admin dashboard can authenticate anyone.
func (a Admin) Enabled() bool {
	return a.ClerkSecretKey != "" && a.ClerkPublishableKey != "" &&
		(len(a.AllowedEmails) > 0 || len(a.AllowedUserIDs) > 0 || a.AdminRole != "")
}

// PollInterval returns the poll interval as a duration.
func (p Poller) PollInterval() time.Duration {
	return time.Duration(p.PollIntervalMS) * time.Millisecond
}

// Enricher is the configuration of cmd/enricher.
type Enricher struct {
	Common
	Redis

	TrackerGRPCAddr  string `env:"TRACKER_GRPC_ADDR" envDefault:"api:9000"`
	InternalToken    string `env:"INTERNAL_API_TOKEN,required,unset"`
	EnrichIntervalMS int    `env:"ENRICH_INTERVAL_MS" envDefault:"60000"`
	BatchSize        int    `env:"ENRICH_BATCH_SIZE" envDefault:"50"`

	SoundchartsBaseURL string  `env:"SOUNDCHARTS_BASE_URL" envDefault:"https://customer.api.soundcharts.com"`
	SoundchartsAppID   string  `env:"SOUNDCHARTS_APP_ID,required"`
	SoundchartsToken   string  `env:"SOUNDCHARTS_TOKEN,required,unset"`
	SoundchartsRPS     float64 `env:"SOUNDCHARTS_RPS" envDefault:"10"`
	SoundchartsBurst   int     `env:"SOUNDCHARTS_BURST" envDefault:"5"`
	// Concurrency is the number of tracks enriched in parallel.
	Concurrency int `env:"ENRICH_CONCURRENCY" envDefault:"5"`
}

// EnrichInterval returns the sweep interval as a duration.
func (e Enricher) EnrichInterval() time.Duration {
	return time.Duration(e.EnrichIntervalMS) * time.Millisecond
}

// Importer is the configuration of cmd/importer (Directus -> Postgres).
type Importer struct {
	Common
	Postgres

	DirectusURL string `env:"DIRECTUS_URL,required"`
	// DirectusToken is one token, or a comma-separated list tried in order.
	DirectusToken string `env:"DIRECTUS_TOKEN,required,unset"`
	PageSize      int    `env:"IMPORT_PAGE_SIZE" envDefault:"500"`
}

// Migrate is the configuration of cmd/migrate.
type Migrate struct {
	Common
	Postgres
}

// Load parses environment variables into T and validates the result.
func Load[T any]() (T, error) {
	cfg, err := env.ParseAs[T]()
	if err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	if v, ok := any(&cfg).(interface{ validate() error }); ok {
		if err := v.validate(); err != nil {
			return cfg, fmt.Errorf("config: %w", err)
		}
	}
	return cfg, nil
}

func (p *Poller) validate() error {
	if p.PollIntervalMS < 500 {
		return fmt.Errorf("POLL_INTERVAL_MS must be >= 500, got %d", p.PollIntervalMS)
	}
	return p.QueuePrefetch.validate()
}

func (e *Enricher) validate() error {
	if e.EnrichIntervalMS < 1000 {
		return fmt.Errorf("ENRICH_INTERVAL_MS must be >= 1000, got %d", e.EnrichIntervalMS)
	}
	if e.BatchSize <= 0 || e.BatchSize > 500 {
		return fmt.Errorf("ENRICH_BATCH_SIZE must be in 1..500, got %d", e.BatchSize)
	}
	if e.SoundchartsRPS <= 0 {
		return fmt.Errorf("SOUNDCHARTS_RPS must be > 0")
	}
	if e.SoundchartsBurst <= 0 {
		return fmt.Errorf("SOUNDCHARTS_BURST must be > 0")
	}
	if e.Concurrency <= 0 || e.Concurrency > 50 {
		return fmt.Errorf("ENRICH_CONCURRENCY must be in 1..50, got %d", e.Concurrency)
	}
	return nil
}

func (a *API) validate() error {
	if len(a.InternalToken) < 16 {
		return fmt.Errorf("INTERNAL_API_TOKEN must be at least 16 characters")
	}
	if a.Spotify.TokenLifetimeDays <= 0 {
		return fmt.Errorf("SPOTIFY_TOKEN_LIFETIME_DAYS must be > 0")
	}
	if a.Spotify.AuthorizedAt != "" {
		if _, err := time.Parse(time.DateOnly, a.Spotify.AuthorizedAt); err != nil {
			return fmt.Errorf("SPOTIFY_AUTHORIZED_AT must be YYYY-MM-DD: %w", err)
		}
	}
	if a.Spotify.RedirectURI == "" {
		a.Spotify.RedirectURI = strings.TrimRight(a.PublicURL, "/") + "/admin/api/spotify/callback"
	}
	if a.TrustedProxyHops < 0 {
		return fmt.Errorf("TRUSTED_PROXY_HOPS must be >= 0")
	}
	if a.EnrichMaxAttempts <= 0 {
		return fmt.Errorf("ENRICH_MAX_ATTEMPTS must be > 0")
	}
	if a.ReplayResetThreshold <= 0 || a.ReplayResetThreshold > 10*time.Minute {
		return fmt.Errorf("REPLAY_RESET_THRESHOLD must be between 1ms and 10m")
	}
	if a.PlayCountMinFraction < 0.05 || a.PlayCountMinFraction > 0.9 {
		return fmt.Errorf("PLAY_COUNT_MIN_FRACTION must be between 0.05 and 0.9")
	}
	if a.PlayResumeWindow < time.Minute || a.PlayResumeWindow > 30*24*time.Hour {
		return fmt.Errorf("PLAY_RESUME_WINDOW must be between 1m and 720h")
	}
	if a.MCP.AdminToken != "" && len(a.MCP.AdminToken) < minMCPAdminTokenLen {
		return fmt.Errorf("MCP_ADMIN_TOKEN must be at least %d characters", minMCPAdminTokenLen)
	}
	if a.MCP.RateLimitPerMinute < 0 {
		return fmt.Errorf("MCP_RATE_LIMIT_PER_MINUTE must be >= 0")
	}
	if err := a.Lyrics.validate(); err != nil {
		return err
	}
	if err := a.Translate.validate(); err != nil {
		return err
	}
	// An empty BRAND_API_URL disables the brand API, so the default applies
	// only when the variable is not set at all.
	if _, set := os.LookupEnv("BRAND_API_URL"); !set {
		a.Brand.APIURL = DefaultBrandAPIURL
	}
	a.Brand.APIURL = strings.TrimSpace(a.Brand.APIURL)
	switch strings.ToLower(a.Brand.APIURL) {
	case "off", "none", "disabled":
		a.Brand.APIURL = "" // explicit opt-out, for hosts that drop empty values
	}
	if a.Brand.Refresh < 10*time.Second || a.Brand.Refresh > 24*time.Hour {
		return fmt.Errorf("BRAND_REFRESH must be between 10s and 24h")
	}
	return nil
}

func (i *Importer) validate() error {
	if i.PageSize <= 0 || i.PageSize > 5000 {
		return fmt.Errorf("IMPORT_PAGE_SIZE must be in 1..5000")
	}
	return nil
}

// AuthorizedAtTime parses SPOTIFY_AUTHORIZED_AT (nil when unset or invalid).
func (s Spotify) AuthorizedAtTime() *time.Time {
	if s.AuthorizedAt == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, s.AuthorizedAt)
	if err != nil {
		return nil
	}
	return &t
}
