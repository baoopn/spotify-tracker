package analytics

import "net/http"

// Country is the visitor's country as page stats record it: Cloudflare's
// CF-IPCountry (ISO 3166-1 alpha-2), or "" when unknown (XX, T1, missing).
func Country(req *http.Request) string { return countryOf(req) }

// DeviceBrowser reduces a User-Agent to the device class (mobile, desktop)
// and browser family page stats use.
func DeviceBrowser(ua string) (device, browser string) { return deviceOf(0, ua), browserOf(ua) }
