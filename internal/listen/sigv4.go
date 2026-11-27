// Package listen serves "listen along": it finds the audio file of the song
// playing now in an S3-compatible bucket and hands out short-lived presigned
// URLs for it.
package listen

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/spotify-tracker/internal/config"
)

// Signer presigns S3 requests with AWS Signature Version 4 query
// parameters (no request body, UNSIGNED-PAYLOAD).
type Signer struct {
	Endpoint    *url.URL
	Region      string
	Bucket      string
	AccessKeyID string
	Secret      string
	// PathStyle puts the bucket in the path instead of the host name.
	PathStyle bool
}

// NewSigner builds a Signer from validated S3 settings, or nil when S3 is
// not configured.
func NewSigner(cfg config.S3) *Signer {
	if !cfg.Configured() {
		return nil
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Host == "" {
		return nil
	}
	return &Signer{Endpoint: &url.URL{Scheme: u.Scheme, Host: u.Host}, Region: cfg.Region, Bucket: cfg.Bucket,
		AccessKeyID: cfg.AccessKeyID, Secret: cfg.SecretAccessKey, PathStyle: cfg.ForcePathStyle}
}

const (
	sigAlgorithm = "AWS4-HMAC-SHA256"
	minPresign   = time.Second
	maxPresign   = 7 * 24 * time.Hour // the SigV4 limit
)

// Presign returns a URL for method on key, valid for ttl (clamped to 1s..7d)
// from now. The URL carries a credential: never log it.
func (s *Signer) Presign(method, key string, ttl time.Duration, now time.Time) string {
	ttl = min(max(ttl, minPresign), maxPresign)
	now = now.UTC()
	date := now.Format("20060102")
	stamp := now.Format("20060102T150405Z")
	scope := date + "/" + s.Region + "/s3/aws4_request"

	host := s.Endpoint.Host
	path := "/" + encodeURI(key, true)
	if s.PathStyle {
		path = "/" + encodeURI(s.Bucket, false) + path
	} else {
		host = s.Bucket + "." + host
	}

	// Already in sorted order.
	query := strings.Join([]string{
		"X-Amz-Algorithm=" + sigAlgorithm,
		"X-Amz-Credential=" + encodeURI(s.AccessKeyID+"/"+scope, false),
		"X-Amz-Date=" + stamp,
		"X-Amz-Expires=" + strconv.FormatInt(int64(ttl/time.Second), 10),
		"X-Amz-SignedHeaders=host",
	}, "&")
	canonical := method + "\n" + path + "\n" + query + "\nhost:" + host + "\n\nhost\nUNSIGNED-PAYLOAD"
	sum := sha256.Sum256([]byte(canonical))
	toSign := sigAlgorithm + "\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	k := hmacSHA256([]byte("AWS4"+s.Secret), date)
	k = hmacSHA256(k, s.Region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))

	return s.Endpoint.Scheme + "://" + host + path + "?" + query + "&X-Amz-Signature=" + sig
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// encodeURI percent-encodes every byte except the RFC 3986 unreserved
// characters (and "/" when keepSlash), as SigV4 requires.
func encodeURI(s string, keepSlash bool) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '-', c == '.', c == '_', c == '~', keepSlash && c == '/':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&15])
		}
	}
	return b.String()
}
