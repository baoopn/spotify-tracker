package staticsite

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func hash(script string) string {
	sum := sha256.Sum256([]byte(script))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func directives(csp string) map[string]string {
	out := map[string]string{}
	for _, d := range strings.Split(csp, "; ") {
		name, value, _ := strings.Cut(d, " ")
		out[name] = value
	}
	return out
}

func cspFor(t *testing.T, s *Site, path string) map[string]string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatalf("GET %s: no CSP (%d)", path, rec.Code)
	}
	return directives(csp)
}

func TestPerPageCSP(t *testing.T) {
	s := New(fstest.MapFS{
		"index.html":       {Data: []byte("<script>home()</script>")},
		"404.html":         {Data: []byte("<script>lost()</script>")},
		"listen.html":      {Data: []byte("<script>listen()</script>")},
		"about/index.html": {Data: []byte("<script>about()</script>")},
	}, Options{
		ExtraCSP: map[string]string{"connect-src": "https://baoopn.com", "frame-src": "https://open.spotify.com"},
		PageCSP: map[string]map[string]string{"listen.html": {
			"connect-src": "https://clerk.example.com", "frame-src": "https://clerk.example.com",
			"media-src": "https://s3.example.com",
		}},
	})

	home := cspFor(t, s, "/")
	if !strings.Contains(home["script-src"], hash("home()")) || !strings.Contains(home["script-src"], hash("lost()")) ||
		strings.Contains(home["script-src"], hash("listen()")) {
		t.Errorf("home script-src = %s", home["script-src"])
	}
	if home["media-src"] != "'self'" || strings.Contains(home["connect-src"], "clerk") {
		t.Errorf("home = %v", home)
	}

	listen := cspFor(t, s, "/listen")
	if listen["script-src"] != "'self' "+hash("listen()") {
		t.Errorf("listen script-src = %s", listen["script-src"])
	}
	if listen["connect-src"] != "'self' https://baoopn.com https://clerk.example.com" ||
		listen["frame-src"] != "https://open.spotify.com https://clerk.example.com" ||
		listen["media-src"] != "'self' https://s3.example.com" || listen["frame-ancestors"] != "'none'" {
		t.Errorf("listen = %v", listen)
	}
	if got := cspFor(t, s, "/listen.html"); got["media-src"] != "'self' https://s3.example.com" {
		t.Errorf("listen.html = %v", got)
	}

	// Other prerendered pages hash their own scripts, without listen's sources.
	about := cspFor(t, s, "/about")
	if about["script-src"] != "'self' "+hash("about()") || about["media-src"] != "'self'" {
		t.Errorf("about = %v", about)
	}
	// The 404 page keeps the site CSP.
	if nf := cspFor(t, s, "/nope"); nf["script-src"] != home["script-src"] {
		t.Errorf("404 = %v", nf)
	}
}

// A host with a port wildcard (Clerk's https://*.protect.clerk.com:*) is kept
// as written.
func TestBuildCSPPortWildcard(t *testing.T) {
	d := directives(BuildCSP(nil, map[string]string{"connect-src": "https://*.protect.clerk.com:*"}))
	if d["connect-src"] != "'self' https://*.protect.clerk.com:*" {
		t.Errorf("connect-src = %q", d["connect-src"])
	}
}

func TestMerge(t *testing.T) {
	got := merge(map[string]string{"a": "x", "b": "y"}, map[string]string{"b": "z", "c": "w", "d": ""})
	if len(got) != 3 || got["a"] != "x" || got["b"] != "y z" || got["c"] != "w" {
		t.Errorf("merge = %v", got)
	}
	if got := merge(nil, nil); len(got) != 0 {
		t.Errorf("merge(nil) = %v", got)
	}
}

func TestIndexPageCSP(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":  {Data: []byte("<script>home()</script>")},
		"404.html":    {Data: []byte("<script>lost()</script>")},
		"listen.html": {Data: []byte("<script>listen()</script>")},
	}
	opts := Options{
		ExtraCSP: map[string]string{"connect-src": "https://baoopn.com"},
		PageCSP: map[string]map[string]string{
			"index.html":  {"connect-src": "https://clerk.example.com", "img-src": "https://img.clerk.com"},
			"listen.html": {"connect-src": "https://clerk.example.com", "media-src": "https://s3.example.com"},
		},
	}
	s := New(dist, opts)
	home := cspFor(t, s, "/")
	if home["connect-src"] != "'self' https://baoopn.com https://clerk.example.com" || !strings.Contains(home["img-src"], "https://img.clerk.com") ||
		home["media-src"] != "'self'" || !strings.Contains(home["script-src"], hash("home()")) {
		t.Errorf("/ = %v", home)
	}
	if idx := cspFor(t, s, "/index.html"); idx["connect-src"] != home["connect-src"] {
		t.Errorf("/index.html = %v", idx)
	}
	// The 404 page keeps the site CSP.
	if nf := cspFor(t, s, "/nope"); nf["connect-src"] != "'self' https://baoopn.com" || nf["script-src"] != home["script-src"] {
		t.Errorf("404 = %v", nf)
	}
	// /listen keeps its own CSP.
	if l := cspFor(t, s, "/listen"); l["media-src"] != "'self' https://s3.example.com" || l["script-src"] != "'self' "+hash("listen()") {
		t.Errorf("/listen = %v", l)
	}
	// Without an index entry, / is unchanged.
	delete(opts.PageCSP, "index.html")
	if plain := cspFor(t, New(dist, opts), "/"); plain["connect-src"] != "'self' https://baoopn.com" {
		t.Errorf("/ without entry = %v", plain)
	}
	// A missing build serves the placeholder with the site CSP.
	opts.PageCSP["index.html"] = map[string]string{"connect-src": "https://clerk.example.com"}
	if ph := cspFor(t, New(fstest.MapFS{}, opts), "/"); ph["connect-src"] != "'self' https://baoopn.com" {
		t.Errorf("placeholder = %v", ph)
	}
}

// The SPA fallback serves index.html, so it gets the index CSP.
func TestIndexPageCSPSPAFallback(t *testing.T) {
	s := New(fstest.MapFS{"index.html": {Data: []byte("<p>app</p>")}}, Options{SPA: true,
		PageCSP: map[string]map[string]string{"index.html": {"frame-src": "https://clerk.example.com"}}})
	if got := cspFor(t, s, "/some/route"); got["frame-src"] != "https://clerk.example.com" {
		t.Errorf("fallback = %v", got)
	}
}
