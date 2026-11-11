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
// fallback and a strict CSP that also allows Clerk's Frontend API (derived
// from the publishable key; Clerk's wildcard hosts only without one) and
// Cloudflare Turnstile.
func MountSPA(r chi.Router, dist fs.FS, clerkPublishableKey string) {
	clerk := clerkSources(clerkPublishableKey)
	site := staticsite.New(dist, staticsite.Options{
		Prefix: "/admin",
		SPA:    true,
		ExtraCSP: map[string]string{
			"script-src": clerk,
			// The sidebar credit fetches https://baoopn.com/api/v1/credit and
			// shows the brand mark from media.baoopn.com.
			"connect-src": clerk + " https://baoopn.com",
			"frame-src":   clerk,
			"img-src":     "https://img.clerk.com https://media.baoopn.com",
		},
		Placeholder: "The admin UI was not built. Run <code>pnpm --dir web/admin build</code>.",
	})
	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	r.Get("/admin/*", site.ServeHTTP)
	r.Head("/admin/*", site.ServeHTTP)
}

// clerkSources is the CSP source list for Clerk and Turnstile: the Frontend
// API host from the publishable key, or Clerk's wildcard hosts only when the
// key carries no usable host.
func clerkSources(pk string) string {
	const turnstile = "https://challenges.cloudflare.com"
	if host := ClerkFrontendHost(pk); host != "" {
		return "https://" + host + " " + turnstile
	}
	return "https://*.clerk.accounts.dev https://*.clerk.com " + turnstile
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
