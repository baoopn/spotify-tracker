package analytics

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

// Country is the visitor's country as page stats record it: Cloudflare's
// CF-IPCountry (ISO 3166-1 alpha-2), or "" when unknown (XX, T1, missing).
func Country(req *http.Request) string { return countryOf(req) }

// DeviceBrowser reduces a User-Agent to the device class (mobile, desktop)
// and browser family page stats use.
func DeviceBrowser(ua string) (device, browser string) { return deviceOf(0, ua), browserOf(ua) }

// HashIP is a salted, one-way hash of a client IP for scope (e.g. a rate
// limit), as analytics hashes clients: the IP cannot be read back.
func HashIP(salt, scope, ip string) string {
	sum := sha256.Sum256([]byte(salt + "|" + scope + "|" + ip))
	return hex.EncodeToString(sum[:16])
}

// HashIP hashes a client IP for scope with the recorder's secret salt.
func (r *Recorder) HashIP(scope, ip string) string { return HashIP(r.salt, scope, ip) }
