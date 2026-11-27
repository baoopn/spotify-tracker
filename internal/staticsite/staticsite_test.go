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

func TestMerge(t *testing.T) {
	got := merge(map[string]string{"a": "x", "b": "y"}, map[string]string{"b": "z", "c": "w", "d": ""})
	if len(got) != 3 || got["a"] != "x" || got["b"] != "y z" || got["c"] != "w" {
		t.Errorf("merge = %v", got)
	}
	if got := merge(nil, nil); len(got) != 0 {
		t.Errorf("merge(nil) = %v", got)
	}
}
