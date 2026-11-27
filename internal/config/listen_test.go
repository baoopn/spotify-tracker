package config

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func validListen() Listen {
	return Listen{Enabled: true, AudioURLTTL: 10 * time.Minute, AudioExtensions: []string{"mp3", "m4a"},
		S3: S3{Endpoint: "https://s3.example.com", Region: "us-east-1", Bucket: "music",
			AccessKeyID: "AKID", SecretAccessKey: "secret", Prefix: "audio/", ForcePathStyle: true}}
}

func TestListenValidate(t *testing.T) {
	cases := []struct {
		name    string
		mut     func(*Listen)
		dev     bool
		role    string
		wantErr string
	}{
		{"valid", func(*Listen) {}, false, "admin", ""},
		{"disabled skips checks", func(l *Listen) { l.Enabled = false; l.AudioURLTTL = 0 }, false, "admin", ""},
		{"listener admin role, disabled", func(l *Listen) { l.Enabled = false }, false, "listener", "ADMIN_ROLE"},
		{"listener admin role", func(*Listen) {}, false, "listener", "ADMIN_ROLE"},
		{"ttl too short", func(l *Listen) { l.AudioURLTTL = 30 * time.Second }, false, "admin", "LISTEN_AUDIO_URL_TTL"},
		{"ttl too long", func(l *Listen) { l.AudioURLTTL = 2 * time.Hour }, false, "admin", "LISTEN_AUDIO_URL_TTL"},
		{"bad ext", func(l *Listen) { l.AudioExtensions = []string{"mp3", "../x"} }, false, "admin", "LISTEN_AUDIO_EXTENSIONS"},
		{"long ext", func(l *Listen) { l.AudioExtensions = []string{"abcdefghi"} }, false, "admin", "LISTEN_AUDIO_EXTENSIONS"},
		{"no ext", func(l *Listen) { l.AudioExtensions = []string{" ", ""} }, false, "admin", "1 to 10"},
		{"too many ext", func(l *Listen) {
			l.AudioExtensions = strings.Split("a,b,c,d,e,f,g,h,i,j,k", ",")
		}, false, "admin", "1 to 10"},
		{"no storage at all", func(l *Listen) { l.S3 = S3{Region: "us-east-1"} }, false, "admin", ""},
		{"partial storage", func(l *Listen) { l.Bucket, l.SecretAccessKey = "", "" }, false, "admin",
			"S3_BUCKET, S3_SECRET_ACCESS_KEY"},
		{"http endpoint", func(l *Listen) { l.Endpoint = "http://minio:9000" }, false, "admin", "https"},
		{"http endpoint dev", func(l *Listen) { l.Endpoint = "http://minio:9000" }, true, "admin", ""},
		{"endpoint with path", func(l *Listen) { l.Endpoint = "https://s3.example.com/bucket" }, false, "admin", "no path"},
		{"endpoint with query", func(l *Listen) { l.Endpoint = "https://s3.example.com?x=1" }, false, "admin", "no path"},
		{"endpoint with fragment", func(l *Listen) { l.Endpoint = "https://s3.example.com#x" }, false, "admin", "no path"},
		{"endpoint no host", func(l *Listen) { l.Endpoint = "s3.example.com" }, false, "admin", "absolute"},
		{"empty region", func(l *Listen) { l.Region = " " }, false, "admin", "S3_REGION"},
		{"prefix slash", func(l *Listen) { l.Prefix = "/audio" }, false, "admin", "S3_PREFIX"},
		{"prefix dotdot", func(l *Listen) { l.Prefix = "a/../b/" }, false, "admin", "S3_PREFIX"},
		{"prefix long", func(l *Listen) { l.Prefix = strings.Repeat("a", 257) }, false, "admin", "S3_PREFIX"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := validListen()
			c.mut(&l)
			err := l.validate(c.dev, c.role)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("err = %v, want containing %q", err, c.wantErr)
			}
		})
	}
}

func TestListenNormalizesExtensions(t *testing.T) {
	l := validListen()
	l.AudioExtensions = []string{" MP3", "", "Flac "}
	l.Endpoint = "https://s3.example.com/"
	if err := l.validate(false, "admin"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.AudioExtensions, []string{"mp3", "flac"}) || l.Endpoint != "https://s3.example.com" {
		t.Errorf("normalized = %v %q", l.AudioExtensions, l.Endpoint)
	}
}

func TestS3Origin(t *testing.T) {
	s := validListen().S3
	if got := s.Origin(); got != "https://s3.example.com" {
		t.Errorf("path style origin = %q", got)
	}
	s.ForcePathStyle = false
	if got := s.Origin(); got != "https://music.s3.example.com" {
		t.Errorf("virtual host origin = %q", got)
	}
	s.SecretAccessKey = ""
	if s.Configured() || s.Origin() != "" {
		t.Error("partial S3 counts as configured")
	}
}

func TestLoadAPIListenDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("SECRETS_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")
	cfg, err := Load[API]()
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.Listen
	if l.Enabled || l.AudioURLTTL != 10*time.Minute || !slices.Equal(l.AudioExtensions, []string{"mp3", "m4a", "flac", "ogg", "opus"}) ||
		l.Region != "us-east-1" || !l.ForcePathStyle || l.Configured() {
		t.Errorf("defaults = %+v", l)
	}
}
