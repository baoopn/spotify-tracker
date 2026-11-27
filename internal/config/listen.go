package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// listenerRole is the Clerk role given to invited listeners. It must never
// be the admin role.
const listenerRole = "listener"

// Listen configures "listen along" (/listen): invited people hear the song
// playing now, from audio files in an S3-compatible bucket.
type Listen struct {
	// Enabled serves /listen and the /api/v1/listen/* endpoints.
	Enabled bool `env:"LISTEN_ENABLED" envDefault:"false"`
	// AudioURLTTL is the lifetime of a presigned audio URL (1m..1h); a URL
	// lives at least until the song ends plus a minute, capped at 1h.
	AudioURLTTL time.Duration `env:"LISTEN_AUDIO_URL_TTL" envDefault:"10m"`
	// AudioExtensions are the file extensions tried, in order, for
	// {S3_PREFIX}{spotify_track_id}.{ext}.
	AudioExtensions []string `env:"LISTEN_AUDIO_EXTENSIONS" envSeparator:"," envDefault:"mp3,m4a,flac,ogg,opus"`
	// AudioSyncInterval is how often the bucket is listed to record the
	// uploaded files (0 = off; 1m..24h).
	AudioSyncInterval time.Duration `env:"LISTEN_AUDIO_SYNC_INTERVAL" envDefault:"15m"`
	S3
}

// S3 is an S3-compatible bucket (AWS S3, Cloudflare R2, MinIO, ...).
type S3 struct {
	Endpoint        string `env:"S3_ENDPOINT"`
	Region          string `env:"S3_REGION" envDefault:"us-east-1"`
	Bucket          string `env:"S3_BUCKET"`
	AccessKeyID     string `env:"S3_ACCESS_KEY_ID,unset"`
	SecretAccessKey string `env:"S3_SECRET_ACCESS_KEY,unset"`
	// Prefix is put before every object key, e.g. "audio/".
	Prefix string `env:"S3_PREFIX"`
	// ForcePathStyle uses https://endpoint/bucket/key instead of
	// https://bucket.endpoint/key (MinIO and R2 need path style).
	ForcePathStyle bool `env:"S3_FORCE_PATH_STYLE" envDefault:"true"`
}

// Configured reports whether every required S3 setting is set.
func (s S3) Configured() bool {
	return s.Endpoint != "" && s.Bucket != "" && s.AccessKeyID != "" && s.SecretAccessKey != ""
}

// Origin is the scheme and host audio URLs are served from (for the CSP
// media-src), or "" when S3 is not configured.
func (s S3) Origin() string {
	if !s.Configured() {
		return ""
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Host
	if !s.ForcePathStyle {
		host = s.Bucket + "." + host
	}
	return u.Scheme + "://" + host
}

var audioExt = regexp.MustCompile(`^[a-z0-9]{1,8}$`)

// validate checks the listen-along settings. adminRole is ADMIN_ROLE, which
// may never be the listener role (whether or not the feature is on).
func (l *Listen) validate(dev bool, adminRole string) error {
	if adminRole == listenerRole {
		return fmt.Errorf("ADMIN_ROLE must not be %q: that role is given to listen-along guests", listenerRole)
	}
	if !l.Enabled {
		return nil
	}
	if l.AudioURLTTL < time.Minute || l.AudioURLTTL > time.Hour {
		return fmt.Errorf("LISTEN_AUDIO_URL_TTL must be between 1m and 1h")
	}
	exts := make([]string, 0, len(l.AudioExtensions))
	for _, e := range l.AudioExtensions {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !audioExt.MatchString(e) {
			return fmt.Errorf("LISTEN_AUDIO_EXTENSIONS: %q is not a file extension (1-8 letters or digits)", e)
		}
		exts = append(exts, e)
	}
	if len(exts) < 1 || len(exts) > 10 {
		return fmt.Errorf("LISTEN_AUDIO_EXTENSIONS must list 1 to 10 extensions")
	}
	l.AudioExtensions = exts
	if l.AudioSyncInterval != 0 && (l.AudioSyncInterval < time.Minute || l.AudioSyncInterval > 24*time.Hour) {
		return fmt.Errorf("LISTEN_AUDIO_SYNC_INTERVAL must be 0 (off) or between 1m and 24h")
	}
	return l.S3.validate(dev)
}

func (s *S3) validate(dev bool) error {
	s.Endpoint = strings.TrimRight(strings.TrimSpace(s.Endpoint), "/")
	s.Bucket = strings.TrimSpace(s.Bucket)
	s.Region = strings.TrimSpace(s.Region)
	set := map[string]bool{
		"S3_ENDPOINT": s.Endpoint != "", "S3_BUCKET": s.Bucket != "",
		"S3_ACCESS_KEY_ID": s.AccessKeyID != "", "S3_SECRET_ACCESS_KEY": s.SecretAccessKey != "",
	}
	var missing []string
	for _, name := range []string{"S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"} {
		if !set[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 4 {
		return nil // no storage yet: the audio endpoint answers storage_unconfigured
	}
	if len(missing) > 0 {
		return fmt.Errorf("S3 is partly configured: set %s", strings.Join(missing, ", "))
	}
	u, err := url.Parse(s.Endpoint)
	switch {
	case err != nil || u.Host == "":
		return fmt.Errorf("S3_ENDPOINT must be an absolute https URL")
	case u.Scheme != "https" && (!dev || u.Scheme != "http"):
		return fmt.Errorf("S3_ENDPOINT must use https (http only when APP_ENV=development)")
	case u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery:
		return fmt.Errorf("S3_ENDPOINT must be a scheme and host only, with no path, query or fragment")
	case s.Region == "":
		return fmt.Errorf("S3_REGION must not be empty")
	case len(s.Prefix) > 256:
		return fmt.Errorf("S3_PREFIX must be at most 256 characters")
	case strings.HasPrefix(s.Prefix, "/"):
		return fmt.Errorf("S3_PREFIX must not start with /")
	}
	for _, seg := range strings.Split(s.Prefix, "/") {
		if seg == ".." {
			return fmt.Errorf("S3_PREFIX must not contain a .. segment")
		}
	}
	return nil
}

// Turnstile is the Cloudflare Turnstile widget on "Request an invite" at
// /listen. It is on only when both keys are set.
type Turnstile struct {
	// SiteKey is public (sent to the page in /api/v1/listen/config).
	SiteKey string `env:"TURNSTILE_SITE_KEY"`
	// SecretKey verifies tokens with Cloudflare. Never logged.
	SecretKey string `env:"TURNSTILE_SECRET_KEY,unset"`
}

// Enabled reports whether both keys are set.
func (t Turnstile) Enabled() bool {
	return strings.TrimSpace(t.SiteKey) != "" && strings.TrimSpace(t.SecretKey) != ""
}
