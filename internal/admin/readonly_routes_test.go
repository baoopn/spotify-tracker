package admin

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// readOnlyAllowed are the only admin routes the MCP may reach.
var readOnlyAllowed = map[string]bool{
	"GET /overview": true, "GET /spotify": true, "GET /history": true, "GET /tracks": true,
	"GET /tracks/{id}": true, "GET /artists": true, "GET /playlists": true, "GET /top-playlists": true,
	"GET /top-playlists/{period}/{start}": true, "GET /backups": true, "GET /analytics": true,
	"GET /analytics/pages": true, "GET /tracks/{id}/lyrics": true, "GET /lyrics/search": true,
	"GET /lyrics/translation-queue": true, "GET /lyrics/queue": true,
	// The only writes: they only reorder the translation and lyrics queues.
	"POST /tracks/{id}/translation/requeue": true,
	"POST /tracks/{id}/lyrics/requeue":      true,
}

var routeParam = regexp.MustCompile(`\{[^}]+\}`)

// TestReadOnlyHandlerExcludesEveryOtherAdminRoute walks the full admin
// router (as mounted by Mount) and checks that every route outside the
// read-only allow-list, with any method, is 404/405 on ReadOnlyHandler. New
// write routes added to Mount are covered automatically.
func TestReadOnlyHandlerExcludesEveryOtherAdminRoute(t *testing.T) {
	full := chi.NewRouter()
	Mount(full, &API{}, nil)
	ro := ReadOnlyHandler(&API{})

	seen := 0
	err := chi.Walk(full, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		rel := strings.TrimSuffix(strings.TrimPrefix(route, "/admin/api"), "/")
		if rel == "" || rel == "/*" {
			return nil
		}
		if readOnlyAllowed[method+" "+rel] {
			seen++
			return nil
		}
		path := routeParam.ReplaceAllStringFunc(rel, func(p string) string {
			switch p {
			case "{period}":
				return "week"
			case "{start}":
				return "2026-11-16T00:00:00Z"
			}
			return "x"
		})
		rec := httptest.NewRecorder()
		ro.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s reachable through ReadOnlyHandler: %d", method, path, rec.Code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != len(readOnlyAllowed) {
		t.Errorf("found %d of the %d allowed routes in Mount; the allow-list is stale", seen, len(readOnlyAllowed))
	}

	// The MCP settings routes, with every method.
	for _, p := range []string{"/mcp", "/mcp/token", "/settings/email", "/settings/email/test", "/config",
		"/spotify/callback", "/auth/me", "/audit", "/spotify/authorize", "/backups/run"} {
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
			http.MethodPatch, http.MethodDelete} {
			rec := httptest.NewRecorder()
			ro.ServeHTTP(rec, httptest.NewRequest(m, p, nil))
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 404/405", m, p, rec.Code)
			}
		}
	}
	// Allowed read paths still refuse writes.
	for k := range readOnlyAllowed {
		method, rel, _ := strings.Cut(k, " ")
		if method != http.MethodGet {
			continue
		}
		path := routeParam.ReplaceAllString(rel, "x")
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			rec := httptest.NewRecorder()
			ro.ServeHTTP(rec, httptest.NewRequest(m, path, nil))
			if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 405", m, path, rec.Code)
			}
		}
	}
}
