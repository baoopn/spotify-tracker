// Package staticsite serves an embedded static frontend build (SvelteKit
// adapter-static output) with a strict Content-Security-Policy, long-lived
// caching for hashed assets and optional SPA fallback.
package staticsite

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

var inlineScript = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.*?)</script>`)

// Options configures a site.
type Options struct {
	// Prefix is the URL path the site is mounted at ("" for the root, or
	// "/admin"). Requests must already be routed to this handler.
	Prefix string
	// SPA serves index.html for unknown extension-less paths (client routing).
	// When false, unknown paths get 404.html (or a plain 404).
	SPA bool
	// Extra CSP sources per directive, e.g. {"script-src": "https://x"}.
	ExtraCSP map[string]string
	// PageCSP adds CSP sources for one prerendered page, keyed by its file
	// name in the build (e.g. "listen.html"), on top of ExtraCSP. An
	// "index.html" entry applies to the index page (and the SPA fallback),
	// never to the 404 page or the placeholder of a missing build.
	PageCSP map[string]map[string]string
	// Placeholder is served as index.html when the build is missing.
	Placeholder string
}

// Site is an http.Handler for one frontend build.
type Site struct {
	dist  fs.FS
	opts  Options
	index []byte
	nf    []byte
	csp   string
	// indexCSP is the CSP of the index page: csp plus PageCSP["index.html"].
	indexCSP string
	// pages is the CSP of each prerendered page other than index.html and
	// 404.html, from that page's own inline scripts.
	pages map[string]string
	files http.Handler
}

// New creates a Site.
func New(dist fs.FS, opts Options) *Site {
	s := &Site{dist: dist, opts: opts, files: http.FileServerFS(dist)}
	idx, err := fs.ReadFile(dist, "index.html")
	built := err == nil
	if !built {
		idx = []byte(`<!doctype html><meta charset="utf-8"><title>Not built</title><p>` + opts.Placeholder + `</p>`)
	}
	s.index = idx
	if nf, err := fs.ReadFile(dist, "404.html"); err == nil {
		s.nf = nf
	}
	scripts := append(append([]byte{}, idx...), s.nf...)
	s.csp = BuildCSP(scripts, opts.ExtraCSP)
	s.indexCSP = s.csp
	if extra, ok := opts.PageCSP["index.html"]; ok && built {
		s.indexCSP = BuildCSP(scripts, merge(opts.ExtraCSP, extra))
	}
	s.pages = map[string]string{}
	_ = fs.WalkDir(dist, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(name) != ".html" || name == "index.html" || name == "404.html" {
			return nil
		}
		if body, err := fs.ReadFile(dist, name); err == nil {
			s.pages[name] = BuildCSP(body, merge(opts.ExtraCSP, opts.PageCSP[name]))
		}
		return nil
	})
	return s
}

// merge joins the sources of a and b per directive.
func merge(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v == "" {
			continue
		}
		if out[k] != "" {
			v = out[k] + " " + v
		}
		out[k] = v
	}
	return out
}

func (s *Site) html(w http.ResponseWriter, status int, body []byte, csp string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.WriteHeader(status)
	_, _ = w.Write(body) //nolint:gosec // body is an embedded build artefact, not user input
}

// ServeHTTP serves files, index.html and the fallback.
func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, s.opts.Prefix)
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "index.html" {
		s.html(w, http.StatusOK, s.index, s.indexCSP)
		return
	}
	clean := path.Clean(p)
	if strings.HasPrefix(clean, "..") {
		s.notFound(w, r)
		return
	}
	// Prerendered routes: /foo → foo.html or foo/index.html.
	for _, cand := range []string{clean, clean + ".html", clean + "/index.html"} {
		st, err := fs.Stat(s.dist, cand)
		if err != nil || st.IsDir() {
			continue
		}
		if strings.HasSuffix(cand, ".html") {
			body, err := fs.ReadFile(s.dist, cand)
			if err == nil {
				csp, ok := s.pages[cand]
				if !ok {
					csp = s.csp
				}
				s.html(w, http.StatusOK, body, csp)
				return
			}
		}
		if strings.HasPrefix(cand, "_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + cand
		s.files.ServeHTTP(w, r2)
		return
	}
	if s.opts.SPA && path.Ext(clean) == "" {
		s.html(w, http.StatusOK, s.index, s.indexCSP)
		return
	}
	s.notFound(w, r)
}

func (s *Site) notFound(w http.ResponseWriter, r *http.Request) {
	if s.nf != nil {
		s.html(w, http.StatusNotFound, s.nf, s.csp)
		return
	}
	http.NotFound(w, r)
}

// BuildCSP builds a strict CSP that allow-lists the hashes of the inline
// scripts found in html plus extra sources.
func BuildCSP(html []byte, extra map[string]string) string {
	hashes := map[string]bool{}
	script := []string{"'self'"}
	for _, m := range inlineScript.FindAllSubmatch(html, -1) {
		body := m[1]
		if len(bytes.TrimSpace(body)) == 0 {
			continue
		}
		sum := sha256.Sum256(body)
		h := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		if !hashes[h] {
			hashes[h] = true
			script = append(script, h)
		}
	}
	directives := []struct{ name, value string }{
		{"default-src", "'self'"},
		{"script-src", strings.Join(script, " ")},
		{"style-src", "'self' 'unsafe-inline'"},
		{"img-src", "'self' data: blob: https://i.scdn.co https://*.scdn.co https://*.spotifycdn.com"},
		{"media-src", "'self'"},
		{"font-src", "'self' data:"},
		{"connect-src", "'self'"},
		{"frame-src", "'none'"},
		{"worker-src", "'self' blob:"},
		{"base-uri", "'self'"},
		{"form-action", "'self'"},
		{"frame-ancestors", "'none'"},
		{"object-src", "'none'"},
	}
	parts := make([]string, 0, len(directives))
	for _, d := range directives {
		v := d.value
		if e, ok := extra[d.name]; ok && e != "" {
			if v == "'none'" {
				v = e
			} else {
				v += " " + e
			}
		}
		parts = append(parts, d.name+" "+v)
	}
	return strings.Join(parts, "; ")
}
