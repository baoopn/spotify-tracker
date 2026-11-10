package brandkit

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// cacheControl is sent on every brand response: short enough that a file
// replaced in the baoopn admin shows up within minutes.
const cacheControl = "public, max-age=300"

const themeColor = "#0b0537"

// Mount registers the brand routes on r: GET/HEAD /brand/{slot}, the root
// favicon files and og:image, /site.webmanifest and /admin/site.webmanifest.
// dist is the built public site, used for root files when nothing is cached.
func Mount(r chi.Router, c *Cache, dist fs.FS) {
	handle := func(pattern string, h http.HandlerFunc) {
		r.Get(pattern, h)
		r.Head(pattern, h)
	}
	handle("/brand/{slot}", func(w http.ResponseWriter, req *http.Request) {
		slot, ok := SlotByName(chi.URLParam(req, "slot"))
		w.Header().Set("Cache-Control", cacheControl)
		if !ok {
			http.NotFound(w, req)
			return
		}
		target := slot.Fallback
		if a, ok := c.Lookup(slot.Name); ok {
			target = a.URL
		}
		http.Redirect(w, req, target, http.StatusFound)
	})
	for _, s := range slots {
		if s.Root {
			handle(s.Fallback, rootFile(s, c, dist))
		}
	}
	handle("/site.webmanifest", manifestHandler(c, manifestNames{"baoopn listening", "listening", ""}))
	handle("/admin/site.webmanifest", manifestHandler(c, manifestNames{"baoopn listening · admin", "listening admin", "./"}))
}

// rootFile redirects to the cached media URL, else serves the static copy.
func rootFile(s Slot, c *Cache, dist fs.FS) http.HandlerFunc {
	name := strings.TrimPrefix(s.Fallback, "/")
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", cacheControl)
		if a, ok := c.Lookup(s.Name); ok {
			http.Redirect(w, req, a.URL, http.StatusFound)
			return
		}
		if dist == nil {
			http.NotFound(w, req)
			return
		}
		b, err := fs.ReadFile(dist, name)
		if err != nil {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", s.Kind.contentType())
		http.ServeContent(w, req, name, time.Time{}, bytes.NewReader(b))
	}
}

type manifestNames struct{ name, short, startURL string }

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

type manifest struct {
	Name            string         `json:"name"`
	ShortName       string         `json:"short_name"`
	Icons           []manifestIcon `json:"icons"`
	ThemeColor      string         `json:"theme_color"`
	BackgroundColor string         `json:"background_color"`
	Display         string         `json:"display"`
	StartURL        string         `json:"start_url,omitempty"`
	Scope           string         `json:"scope,omitempty"`
}

// manifestHandler serves a web manifest whose icons point at the cached
// media URLs (static paths as the fallback).
func manifestHandler(c *Cache, n manifestNames) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		src := func(slot string) string {
			if a, ok := c.Lookup(slot); ok {
				return a.URL
			}
			s, _ := SlotByName(slot)
			return s.Fallback
		}
		m := manifest{
			Name:      n.name,
			ShortName: n.short,
			Icons: []manifestIcon{
				{Src: src("icon_192"), Sizes: "192x192", Type: "image/png"},
				{Src: src("icon_512"), Sizes: "512x512", Type: "image/png"},
				{Src: src("icon_maskable_512"), Sizes: "512x512", Type: "image/png", Purpose: "maskable"},
			},
			ThemeColor:      themeColor,
			BackgroundColor: themeColor,
			Display:         "standalone",
			StartURL:        n.startURL,
			Scope:           n.startURL,
		}
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", cacheControl)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(m)
	}
}
