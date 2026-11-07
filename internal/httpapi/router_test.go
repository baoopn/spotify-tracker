package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/baoopn/spotify-tracker/internal/staticsite"
)

// TestRoutingSplit checks the URL layout: public page at "/", JSON 404s under
// /api, and the site's 404 page for other unknown paths.
func TestRoutingSplit(t *testing.T) {
	site := staticsite.New(fstest.MapFS{
		"index.html":  {Data: []byte("<html>home</html>")},
		"404.html":    {Data: []byte("<html>lost</html>")},
		"favicon.svg": {Data: []byte("<svg/>")},
		"_app/x/a.js": {Data: []byte("1")},
	}, staticsite.Options{})
	h := NewRouter(nil, nil, slog.New(slog.DiscardHandler), Options{Site: site})

	tests := []struct {
		path, wantBody string
		wantCode       int
	}{
		{"/", "home", 200},
		{"/favicon.svg", "<svg/>", 200},
		{"/nope", "lost", 404},
		{"/api/nope", `"not_found"`, 404},
		{"/api/v1/nope", `"not_found"`, 404},
		{"/admin/api/nope", `"not_found"`, 404},
		{"/api/healthz", `"ok"`, 200},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if rec.Code != tt.wantCode || !strings.Contains(rec.Body.String(), tt.wantBody) {
			t.Errorf("GET %s = %d %q; want %d containing %q", tt.path, rec.Code, rec.Body.String(), tt.wantCode, tt.wantBody)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("CSP missing on public page")
	}
}

func TestFormatOffset(t *testing.T) {
	cases := map[int]string{0: "+00:00", -6 * 3600: "-06:00", 5*3600 + 1800: "+05:30", 5*3600 + 2700: "+05:45", -(9*3600 + 1800): "-09:30"}
	for sec, want := range cases {
		if got := formatOffset(sec); got != want {
			t.Errorf("formatOffset(%d) = %q, want %q", sec, got, want)
		}
	}
}

func TestTimezoneEndpoint(t *testing.T) {
	h := NewRouter(nil, nil, slog.New(slog.DiscardHandler), Options{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/timezone?tz=Asia/Kolkata", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"offset":"+05:30"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/timezone?tz=Mars/Base", nil))
	if rec.Code != 400 {
		t.Errorf("bad zone: got %d", rec.Code)
	}
}

func TestTopPlaylistsRouteAndFrameSrc(t *testing.T) {
	site := staticsite.New(fstest.MapFS{"index.html": {Data: []byte("<html>home</html>")}},
		staticsite.Options{ExtraCSP: map[string]string{"frame-src": "https://open.spotify.com"}})
	h := NewRouter(nil, nil, slog.New(slog.DiscardHandler), Options{Site: site})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-src https://open.spotify.com;") {
		t.Errorf("CSP = %q", csp)
	}
	// The route exists (disabled feature: every period empty, no store needed).
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/playlists/top", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"week":[]`) {
		t.Errorf("GET /api/v1/playlists/top = %d %s", rec.Code, rec.Body)
	}
}
