package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/staticsite"
)

func listenCfg() config.API {
	return config.API{
		PublicURL: "https://listening.example.com/",
		Admin: config.Admin{ClerkSecretKey: "sk_test_x", ClerkPublishableKey: "pk_test_Y2xlcmsuZXhhbXBsZS5jb20k",
			AdminRole: "admin"},
		Listen: config.Listen{Enabled: true, AudioURLTTL: 10 * time.Minute, AudioExtensions: []string{"mp3"},
			S3: config.S3{Endpoint: "https://s3.example.com", Region: "us-east-1", Bucket: "music",
				AccessKeyID: "id", SecretAccessKey: "secret", ForcePathStyle: true}},
	}
}

func TestListenPageCSP(t *testing.T) {
	page := publicSiteOptions(listenCfg()).PageCSP["listen.html"]
	if page == nil {
		t.Fatal("no listen.html CSP")
	}
	if !strings.Contains(page["script-src"], "https://clerk.example.com") || !strings.Contains(page["connect-src"], "https://clerk.example.com") ||
		!strings.Contains(page["frame-src"], "https://challenges.cloudflare.com") || page["img-src"] != "https://img.clerk.com" ||
		page["media-src"] != "https://s3.example.com" {
		t.Errorf("listen CSP = %v", page)
	}
	off := listenCfg()
	off.Listen.Enabled = false
	if publicSiteOptions(off).PageCSP != nil {
		t.Error("listen CSP while disabled")
	}
	noS3 := listenCfg()
	noS3.Listen.S3 = config.S3{}
	if _, ok := publicSiteOptions(noS3).PageCSP["listen.html"]["media-src"]; ok {
		t.Error("media-src without storage")
	}
}

// / gets the Clerk sources (the account menu) only when listen along and
// Clerk are on, never the audio bucket; /listen is unchanged.
func TestIndexPageCSP(t *testing.T) {
	site := func(cfg config.API) *staticsite.Site {
		return staticsite.New(fstest.MapFS{
			"index.html":  {Data: []byte("<script>home()</script>")},
			"listen.html": {Data: []byte("<script>listen()</script>")},
		}, publicSiteOptions(cfg))
	}
	csp := func(s *staticsite.Site, path string) map[string]string {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		out := map[string]string{}
		for _, d := range strings.Split(rec.Header().Get("Content-Security-Policy"), "; ") {
			name, value, _ := strings.Cut(d, " ")
			out[name] = value
		}
		return out
	}
	on := site(listenCfg())
	home := csp(on, "/")
	for _, d := range []string{"script-src", "connect-src", "frame-src"} {
		if !strings.Contains(home[d], "https://clerk.example.com") || !strings.Contains(home[d], "https://challenges.cloudflare.com") {
			t.Errorf("/ %s = %q", d, home[d])
		}
	}
	if !strings.Contains(home["img-src"], "https://img.clerk.com") || home["media-src"] != "'self'" ||
		home["frame-ancestors"] != "'none'" || !strings.Contains(home["frame-src"], "https://open.spotify.com") ||
		!strings.Contains(home["connect-src"], "https://baoopn.com") {
		t.Errorf("/ = %v", home)
	}
	// Clerk's protection hosts, on / and /listen alike.
	for _, page := range []string{"/", "/listen"} {
		got := csp(on, page)
		if !strings.Contains(got["script-src"], "https://*.protect.clerk.com") || !strings.Contains(got["frame-src"], "https://*.protect.clerk.com") ||
			!strings.Contains(got["connect-src"], "https://*.protect.clerk.com:*") {
			t.Errorf("%s protect hosts = %v", page, got)
		}
	}
	listen := csp(on, "/listen")
	if listen["media-src"] != "'self' https://s3.example.com" || !strings.Contains(listen["script-src"], "https://clerk.example.com") {
		t.Errorf("/listen = %v", listen)
	}

	for name, cfg := range map[string]config.API{"listen off": func() config.API { c := listenCfg(); c.Listen.Enabled = false; return c }(),
		"no clerk": func() config.API { c := listenCfg(); c.Admin = config.Admin{}; return c }()} {
		if got := csp(site(cfg), "/"); strings.Contains(got["script-src"], "clerk") || strings.Contains(got["img-src"], "img.clerk.com") {
			t.Errorf("%s: / = %v", name, got)
		}
	}
}

func TestListenOptions(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	cfg := listenCfg()
	o := listenOptions(cfg, nil, nil, nil, nil, nil, log)
	if !o.Enabled || o.Auth != nil || o.Audio == nil {
		t.Errorf("options = %+v", o)
	}
	cfg.Listen.S3 = config.S3{}
	if o := listenOptions(cfg, nil, nil, nil, nil, nil, log); o.Audio != nil {
		t.Error("typed nil locator")
	}
	if info := listenInfo(cfg); info.URL != "https://listening.example.com/listen" || !info.Enabled || info.AudioConfigured {
		t.Errorf("info = %+v", info)
	}
}

// Turnstile checks the PUBLIC_URL host, except in development.
func TestListenTurnstileHost(t *testing.T) {
	cfg := listenCfg()
	cfg.Turnstile = config.Turnstile{SiteKey: "1x00000000000000000000AA", SecretKey: "1x0000000000000000000000000000000AA"}
	if !cfg.Turnstile.Enabled() || listenTurnstile(cfg) == nil {
		t.Fatal("turnstile off")
	}
	if got := fmt.Sprintf("%+v", *listenTurnstile(cfg)); !strings.Contains(got, "hostname:listening.example.com") {
		t.Errorf("verifier = %s", got)
	}
	cfg.Env = "development"
	if got := fmt.Sprintf("%+v", *listenTurnstile(cfg)); !strings.Contains(got, "hostname: ") {
		t.Errorf("development verifier = %s", got)
	}
}
