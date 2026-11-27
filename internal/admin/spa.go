package admin

import (
	"encoding/base64"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/staticsite"
)

// MountSPA serves the built dashboard at /admin with client-side routing
// fallback and a strict CSP that also allows Clerk (ClerkCSPSources).
func MountSPA(r chi.Router, dist fs.FS, clerkPublishableKey string) {
	clerk := ClerkCSPSources(clerkPublishableKey)
	site := staticsite.New(dist, staticsite.Options{
		Prefix: "/admin",
		SPA:    true,
		ExtraCSP: map[string]string{
			"script-src": clerk.Script,
			// The sidebar credit fetches https://baoopn.com/api/v1/credit and
			// shows the brand mark from media.baoopn.com.
			"connect-src": clerk.Connect + " https://baoopn.com",
			"frame-src":   clerk.Frame,
			"img-src":     clerk.Img + " https://media.baoopn.com",
		},
		Placeholder: "The admin UI was not built. Run <code>pnpm --dir web/admin build</code>.",
	})
	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	r.Get("/admin/*", site.ServeHTTP)
	r.Head("/admin/*", site.ServeHTTP)
}

// ClerkCSP is the CSP sources Clerk needs, per directive.
type ClerkCSP struct {
	Script, Connect, Frame, Img string
}

const (
	turnstileHost = "https://challenges.cloudflare.com"
	// protectHost serves Clerk's abuse and fraud protection (Device
	// Intelligence). When it is blocked, Clerk Protect cannot run its checks
	// and a policy that requires them blocks sign-ups.
	protectHost = "https://*.protect.clerk.com"
)

// ClerkCSPSources is the CSP for Clerk on the admin SPA, /listen and / (see
// Clerk's CSP guide): the Frontend API host from the publishable key (Clerk's
// wildcard hosts only when the key carries no usable host), Cloudflare
// Turnstile, and Clerk's protection hosts (any port for connections, as the
// guide asks). The pages' base CSP already has worker-src 'self' blob: and
// style-src 'unsafe-inline'.
func ClerkCSPSources(pk string) ClerkCSP {
	fapi := "https://*.clerk.accounts.dev https://*.clerk.com"
	if host := ClerkFrontendHost(pk); host != "" {
		fapi = "https://" + host
	}
	return ClerkCSP{
		Script:  fapi + " " + turnstileHost + " " + protectHost,
		Connect: fapi + " " + turnstileHost + " " + protectHost + ":*",
		Frame:   fapi + " " + turnstileHost + " " + protectHost,
		Img:     "https://img.clerk.com",
	}
}

// ClerkFrontendHost decodes the Frontend API host embedded in a Clerk
// publishable key (pk_test_/pk_live_ + base64("host$")).
func ClerkFrontendHost(pk string) string {
	for _, p := range []string{"pk_test_", "pk_live_"} {
		if strings.HasPrefix(pk, p) {
			raw := strings.TrimRight(strings.TrimPrefix(pk, p), "=")
			b, err := base64.RawStdEncoding.DecodeString(raw)
			if err != nil {
				return ""
			}
			host := strings.TrimSuffix(string(b), "$")
			if host == "" || strings.ContainsAny(host, " ;'\"\n\r\t,") {
				return ""
			}
			return host
		}
	}
	return ""
}
