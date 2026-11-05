package admin

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
)

func TestClerkFrontendHost(t *testing.T) {
	pk := "pk_test_" + base64.RawStdEncoding.EncodeToString([]byte("happy-cat-42.clerk.accounts.dev$"))
	if got := ClerkFrontendHost(pk); got != "happy-cat-42.clerk.accounts.dev" {
		t.Errorf("got %q", got)
	}
	live := "pk_live_" + base64.StdEncoding.EncodeToString([]byte("clerk.example.com$"))
	if got := ClerkFrontendHost(live); got != "clerk.example.com" {
		t.Errorf("live got %q", got)
	}
	evil := "pk_test_" + base64.RawStdEncoding.EncodeToString([]byte("x.com; script-src *$"))
	if got := ClerkFrontendHost(evil); got != "" {
		t.Errorf("unsafe host accepted: %q", got)
	}
	if ClerkFrontendHost("") != "" || ClerkFrontendHost("sk_test_abc") != "" {
		t.Error("non-publishable keys must be ignored")
	}
}

func TestSPA(t *testing.T) {
	script := "document.documentElement.dataset.theme='dark'"
	dist := fstest.MapFS{
		"index.html":              {Data: []byte("<!doctype html><script>" + script + "</script><div id=app></div>")},
		"_app/immutable/entry.js": {Data: []byte("console.log(1)")},
		"favicon.svg":             {Data: []byte("<svg/>")},
	}
	r := chi.NewRouter()
	MountSPA(r, dist, "pk_test_"+base64.RawStdEncoding.EncodeToString([]byte("a.clerk.accounts.dev$")))

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/admin/data/tracks")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "id=app") {
		t.Fatalf("SPA fallback failed: %d %s", rec.Code, rec.Body.String())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	sum := sha256.Sum256([]byte(script))
	if !strings.Contains(csp, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
		t.Errorf("inline script hash missing from CSP: %s", csp)
	}
	if !strings.Contains(csp, "https://a.clerk.accounts.dev") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("unexpected CSP: %s", csp)
	}

	if rec := get("/admin/_app/immutable/entry.js"); rec.Code != 200 ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("immutable asset: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := get("/admin/missing.js"); rec.Code != 404 {
		t.Errorf("missing asset should 404, got %d", rec.Code)
	}
	if rec := get("/admin"); rec.Code != http.StatusMovedPermanently {
		t.Errorf("/admin should redirect, got %d", rec.Code)
	}
	if rec := get("/admin/../go.mod"); rec.Code == 200 && strings.Contains(rec.Body.String(), "module") {
		t.Error("path traversal served a file outside dist")
	}
}

func TestAllowed(t *testing.T) {
	a := &Authenticator{allowedEmails: []string{"me@example.com"}, allowedUserIDs: []string{"user_1"}, adminRole: "admin"}
	cases := []struct {
		u    User
		want bool
	}{
		{User{ID: "user_1"}, true},
		{User{ID: "x", Email: "ME@example.com"}, true},
		{User{ID: "x", Role: "admin"}, true},
		{User{ID: "x", Role: "user", Email: "other@example.com"}, false},
		{User{ID: "x"}, false},
	}
	for _, c := range cases {
		if got := a.allowed(c.u); got != c.want {
			t.Errorf("allowed(%+v) = %v, want %v", c.u, got, c.want)
		}
	}
	if metadataRole([]byte(`{"role":"admin","default_admin":true}`)) != "admin" || metadataRole(nil) != "" {
		t.Error("metadataRole")
	}
}

func TestPartyAllowed(t *testing.T) {
	prod := &Authenticator{parties: []string{"https://listening.example.com"}}
	dev := &Authenticator{parties: []string{"http://127.0.0.1:8080"}, allowLoopback: true}
	cases := []struct {
		a    *Authenticator
		azp  string
		want bool
	}{
		{prod, "https://listening.example.com", true},
		{prod, "http://localhost:5175", false},
		{prod, "https://evil.example", false},
		{prod, "", true},
		{dev, "http://localhost:5175", true},
		{dev, "http://127.0.0.1:4173", true},
		{dev, "http://[::1]:5173", true},
		{dev, "http://localhost.evil.com", false},
		{dev, "javascript:alert(1)", false},
	}
	for _, c := range cases {
		if got := c.a.partyAllowed(c.azp); got != c.want {
			t.Errorf("partyAllowed(%q) = %v, want %v", c.azp, got, c.want)
		}
	}
}
