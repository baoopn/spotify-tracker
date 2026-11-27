package listen

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/config"
)

// The query-string example from the AWS S3 SigV4 documentation
// ("Authenticating Requests: Using Query Parameters").
func TestPresignGoldenVector(t *testing.T) {
	s := &Signer{Endpoint: &url.URL{Scheme: "https", Host: "s3.amazonaws.com"}, Region: "us-east-1",
		Bucket: "examplebucket", AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
		Secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	got := s.Presign("GET", "test.txt", 86400*time.Second, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "https://examplebucket.s3.amazonaws.com/test.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256" +
		"&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request" +
		"&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host" +
		"&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if got != want {
		t.Errorf("presign =\n%s\nwant\n%s", got, want)
	}
}

func TestPresign(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.FixedZone("x", 7*3600))
	path := &Signer{Endpoint: &url.URL{Scheme: "http", Host: "minio:9000"}, Region: "auto", Bucket: "music",
		AccessKeyID: "id", Secret: "secret", PathStyle: true}
	cases := []struct {
		name, key string
		ttl       time.Duration
		prefix    string
		expires   string
	}{
		{"path style", "audio/abc.mp3", time.Minute, "http://minio:9000/music/audio/abc.mp3?", "X-Amz-Expires=60&"},
		{"encodes key", "a b+c/ü.mp3", time.Minute, "http://minio:9000/music/a%20b%2Bc/%C3%BC.mp3?", "X-Amz-Expires=60&"},
		{"ttl floor", "k", 0, "http://minio:9000/music/k?", "X-Amz-Expires=1&"},
		{"ttl cap", "k", 30 * 24 * time.Hour, "http://minio:9000/music/k?", "X-Amz-Expires=604800&"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := path.Presign("GET", c.key, c.ttl, now)
			if !strings.HasPrefix(got, c.prefix) || !strings.Contains(got, c.expires) {
				t.Errorf("presign = %s", got)
			}
			// The date is UTC.
			if !strings.Contains(got, "X-Amz-Date=20261126T050000Z") {
				t.Errorf("date not UTC: %s", got)
			}
		})
	}
	// HEAD and GET sign differently.
	if path.Presign("HEAD", "k", time.Minute, now) == path.Presign("GET", "k", time.Minute, now) {
		t.Error("method is not signed")
	}
}

func TestNewSigner(t *testing.T) {
	if NewSigner(config.S3{Endpoint: "https://x.example.com"}) != nil {
		t.Error("signer for partial config")
	}
	s := NewSigner(config.S3{Endpoint: "https://r2.example.com", Region: "auto", Bucket: "b", AccessKeyID: "i",
		SecretAccessKey: "s", ForcePathStyle: false})
	if s == nil || s.Endpoint.Host != "r2.example.com" || s.PathStyle {
		t.Fatalf("signer = %+v", s)
	}
	if got := s.Presign("GET", "k", time.Minute, time.Unix(0, 0)); !strings.HasPrefix(got, "https://b.r2.example.com/k?") {
		t.Errorf("virtual host url = %s", got)
	}
}
