package analytics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExportedClientHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("CF-IPCountry", " vn ")
	if got := Country(req); got != "VN" {
		t.Errorf("Country = %q", got)
	}
	req.Header.Set("CF-IPCountry", "XX")
	if got := Country(req); got != "" {
		t.Errorf("Country(XX) = %q", got)
	}
	d, b := DeviceBrowser("Mozilla/5.0 (iPhone) AppleWebKit Version/18.0 Mobile/15E148 Safari/604.1")
	if d != "mobile" || b != "Safari" {
		t.Errorf("DeviceBrowser = %q %q", d, b)
	}
	if d, b := DeviceBrowser(""); d != "desktop" || b != "Other" {
		t.Errorf("empty UA = %q %q", d, b)
	}
}

func TestHashIP(t *testing.T) {
	a, b := HashIP("salt", "x", "10.0.0.9"), HashIP("salt2", "x", "10.0.0.9")
	if a == b || len(a) != 32 || a != HashIP("salt", "x", "10.0.0.9") || a == HashIP("salt", "y", "10.0.0.9") {
		t.Errorf("hashes = %s %s", a, b)
	}
}
