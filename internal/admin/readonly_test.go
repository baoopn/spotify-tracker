package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestReadOnlyHandlerRoutes checks the MCP admin handler exposes only GET
// routes, and none of the excluded ones.
func TestReadOnlyHandlerRoutes(t *testing.T) {
	h := ReadOnlyHandler(&API{})
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatal("ReadOnlyHandler is not a chi router")
	}
	n := 0
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		n++
		if method != http.MethodGet {
			t.Errorf("%s %s: only GET routes are allowed", method, route)
		}
		for _, banned := range []string{"/settings", "/auth", "/audit", "/config", "/callback", "/mcp"} {
			if strings.Contains(route, banned) {
				t.Errorf("excluded route exposed: %s %s", method, route)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 12 {
		t.Errorf("got %d routes, want 12", n)
	}

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/settings/email", http.StatusNotFound},
		{http.MethodGet, "/audit", http.StatusNotFound},
		{http.MethodGet, "/auth/me", http.StatusNotFound},
		{http.MethodPost, "/tracks/abc/enrich", http.StatusNotFound},
		{http.MethodPost, "/backups/run", http.StatusNotFound},
		{http.MethodPost, "/overview", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/top-playlists/week/2026-11-16T00:00:00Z", http.StatusMethodNotAllowed},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}
