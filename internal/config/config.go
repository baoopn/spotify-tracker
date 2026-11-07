// Package config loads service configuration from environment variables.
//
// Each binary loads only the struct it needs, so a worker never requires
// secrets that belong to another service.
package config

import (
	"fmt"
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
	Admin
	TopPlaylists

	EnrichMaxAttempts int `env:"ENRICH_MAX_ATTEMPTS" envDefault:"8"`
	// ReplayResetThreshold is how far the progress must jump back on the
	// same track to count as a replay.
	ReplayResetThreshold time.Duration `env:"REPLAY_RESET_THRESHOLD" envDefault:"10s"`
}

// Poller is the configuration of cmd/poller. It holds no Spotify secrets:
// access tokens are obtained from the API over gRPC.
type Poller struct {
	Common
	Redis

	TrackerGRPCAddr string `env:"TRACKER_GRPC_ADDR" envDefault:"api:9000"`
	InternalToken   string `env:"INTERNAL_API_TOKEN,required,unset"`
	PollIntervalMS  int    `env:"POLL_INTERVAL_MS" envDefault:"3000"`
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
}

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
	return nil
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
