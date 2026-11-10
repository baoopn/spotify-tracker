package brandkit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
)

var testDist = fstest.MapFS{
	"favicon.svg":  {Data: []byte("<svg>static</svg>")},
	"favicon.ico":  {Data: []byte("ICO")},
	"og-image.png": {Data: []byte("PNG")},
}

// router mounts the brand routes next to a stub of the admin SPA catch-all.
func router(t *testing.T, warm bool) http.Handler {
	t.Helper()
	c, f := newTestCache(t, nil)
	if warm {
		f.set(http.StatusOK, encode(t, sample(t)), nil)
		if err := c.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	r := chi.NewRouter()
	Mount(r, c, testDist)
	r.Get("/admin/*", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("spa")) })
	return r
}

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestBrandSlotRedirects(t *testing.T) {
	warm, cold := router(t, true), router(t, false)
	cases := []struct {
		h          http.Handler
		method     string
		path, want string
	}{
		{warm, http.MethodGet, "/brand/icon_now_playing_navy", "https://media.baoopn.com/brand/listening/icon-now-playing-navy.824f9e803079a818.svg"},
		{warm, http.MethodHead, "/brand/mark_primary", "https://media.baoopn.com/brand/listening/mark-primary.f79ee816e3b16a3c.svg"},
		{cold, http.MethodGet, "/brand/icon_now_playing_navy", "/icons/now-playing-navy.svg"},
		{cold, http.MethodHead, "/brand/wordmark_cream", "/logo/listening-wordmark-cream.svg"},
		{warm, http.MethodGet, "/favicon.svg", "https://media.baoopn.com/brand/listening/favicon.56da3b28474ad4ed.svg"},
		{warm, http.MethodGet, "/og-image.png", "https://media.baoopn.com/brand/listening/og-image.d50268e7c9cb8053.png"},
	}
	for _, c := range cases {
		rec := do(c.h, c.method, c.path)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != c.want {
			t.Errorf("%s %s = %d %q, want 302 %q", c.method, c.path, rec.Code, rec.Header().Get("Location"), c.want)
		}
		if got := rec.Header().Get("Cache-Control"); got != cacheControl {
			t.Errorf("%s Cache-Control = %q", c.path, got)
		}
	}
}

func TestBrandUnknownSlot(t *testing.T) {
	for _, path := range []string{"/brand/nope", "/brand/"} {
		if rec := do(router(t, true), http.MethodGet, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}
}

func TestRootFilesFallback(t *testing.T) {
	h := router(t, false)
	cases := []struct{ path, ctype, body string }{
		{"/favicon.svg", "image/svg+xml", "<svg>static</svg>"},
		{"/favicon.ico", "image/x-icon", "ICO"},
		{"/og-image.png", "image/png", "PNG"},
	}
	for _, c := range cases {
		rec := do(h, http.MethodGet, c.path)
		if rec.Code != http.StatusOK || rec.Body.String() != c.body ||
			rec.Header().Get("Content-Type") != c.ctype || rec.Header().Get("Cache-Control") != cacheControl {
			t.Errorf("GET %s = %d %q %v", c.path, rec.Code, rec.Body.String(), rec.Header())
		}
		head := do(h, http.MethodHead, c.path)
		if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != c.ctype {
			t.Errorf("HEAD %s = %d len=%d %v", c.path, head.Code, head.Body.Len(), head.Header())
		}
	}
	if rec := do(h, http.MethodGet, "/icon-512.png"); rec.Code != http.StatusNotFound {
		t.Errorf("missing static file = %d, want 404", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/favicon.svg"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rec.Code)
	}
}

func TestManifests(t *testing.T) {
	type icon struct{ Src, Sizes, Type, Purpose string }
	type doc struct {
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		Icons           []icon `json:"icons"`
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
		Display         string `json:"display"`
		StartURL        string `json:"start_url"`
		Scope           string `json:"scope"`
	}
	get := func(h http.Handler, path string) (doc, string) {
		rec := do(h, http.MethodGet, path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/manifest+json" ||
			rec.Header().Get("Cache-Control") != cacheControl {
			t.Fatalf("%s = %d %v", path, rec.Code, rec.Header())
		}
		var d doc
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
			t.Fatalf("%s: %v\n%s", path, err, rec.Body)
		}
		return d, rec.Body.String()
	}
	cold, raw := get(router(t, false), "/site.webmanifest")
	if cold.Name != "baoopn listening" || cold.ShortName != "listening" || cold.ThemeColor != "#0b0537" ||
		cold.BackgroundColor != "#0b0537" || cold.Display != "standalone" {
		t.Errorf("public manifest %+v", cold)
	}
	if strings.Contains(raw, "start_url") || strings.Contains(raw, "scope") {
		t.Errorf("public manifest has start_url/scope:\n%s", raw)
	}
	want := []icon{
		{"/icon-192.png", "192x192", "image/png", ""},
		{"/icon-512.png", "512x512", "image/png", ""},
		{"/icon-maskable-512.png", "512x512", "image/png", "maskable"},
	}
	if len(cold.Icons) != 3 {
		t.Fatalf("icons %+v", cold.Icons)
	}
	for i, w := range want {
		if cold.Icons[i] != w {
			t.Errorf("icon %d = %+v, want %+v", i, cold.Icons[i], w)
		}
	}
	if strings.Count(raw, `"purpose"`) != 1 {
		t.Errorf("purpose should be omitted on plain icons:\n%s", raw)
	}

	warm, _ := get(router(t, true), "/admin/site.webmanifest")
	if warm.Name != "baoopn listening · admin" || warm.ShortName != "listening admin" ||
		warm.StartURL != "./" || warm.Scope != "./" {
		t.Errorf("admin manifest %+v", warm)
	}
	for _, ic := range warm.Icons {
		if !strings.HasPrefix(ic.Src, "https://media.baoopn.com/brand/listening/") {
			t.Errorf("admin icon not from media: %+v", ic)
		}
	}
	if warm.Icons[2].Purpose != "maskable" {
		t.Errorf("maskable purpose lost: %+v", warm.Icons)
	}
	if rec := do(router(t, true), http.MethodHead, "/site.webmanifest"); rec.Code != http.StatusOK {
		t.Errorf("HEAD manifest = %d", rec.Code)
	}
	if rec := do(router(t, true), http.MethodGet, "/admin/other"); rec.Body.String() != "spa" {
		t.Errorf("admin stub not reached: %q", rec.Body)
	}
}
