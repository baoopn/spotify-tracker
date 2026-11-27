package main

import (
	"log/slog"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/admin"
	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/httpapi"
	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/staticsite"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// publicSiteOptions configures the public page: its CSP also allows the
// Spotify embed player used for the top playlists, and the live footer credit
// (fetched from baoopn.com, logo served from media.baoopn.com). With listen
// along on, /listen also allows Clerk (sign-in) and the audio bucket.
func publicSiteOptions(cfg config.API) staticsite.Options {
	opts := staticsite.Options{
		ExtraCSP: map[string]string{
			"frame-src":   "https://open.spotify.com",
			"connect-src": "https://baoopn.com",
			"img-src":     "https://media.baoopn.com",
		},
		Placeholder: "The public page was not built. Run <code>pnpm --dir web/public build</code>.",
	}
	if cfg.Listen.Enabled && cfg.Admin.Enabled() {
		clerk := admin.ClerkCSPSources(cfg.Admin.ClerkPublishableKey)
		page := map[string]string{
			"script-src":  clerk,
			"connect-src": clerk,
			"frame-src":   clerk,
			"img-src":     "https://img.clerk.com",
		}
		if origin := cfg.Listen.Origin(); origin != "" {
			page["media-src"] = origin
		}
		opts.PageCSP = map[string]map[string]string{"listen.html": page}
	}
	return opts
}

// listenOptions configures /api/v1/listen/*. auth is nil when Clerk is not
// configured (the endpoints then answer 503).
func listenOptions(cfg config.API, st *store.Store, rdb *redis.Client, auth *admin.Authenticator, log *slog.Logger) *httpapi.ListenOptions {
	o := &httpapi.ListenOptions{Enabled: cfg.Listen.Enabled, ClerkPublishableKey: cfg.Admin.ClerkPublishableKey}
	if !cfg.Listen.Enabled {
		log.Info("listen along disabled (LISTEN_ENABLED=false)")
		return o
	}
	if auth != nil {
		o.Auth, o.Me = auth.ListenerMiddleware(st), admin.ListenerMe
	} else {
		log.Warn("listen along needs the Clerk admin settings; /api/v1/listen answers 503")
	}
	// Left nil (not a typed nil) without storage: the audio endpoint then
	// answers storage_unconfigured.
	if loc := listen.NewLocator(cfg.Listen, rdb, log.With(slog.String("component", "listen"))); loc != nil {
		o.Audio = loc
	}
	log.Info("listen along enabled", slog.Bool("audio_configured", cfg.Listen.Configured()),
		slog.String("extensions", strings.Join(cfg.Listen.AudioExtensions, ",")),
		slog.Duration("audio_url_ttl", cfg.Listen.AudioURLTTL), slog.Bool("clerk", auth != nil))
	return o
}

// listenInfo describes listen along for the admin Listeners page.
func listenInfo(cfg config.API) admin.ListenInfo {
	return admin.ListenInfo{
		Enabled:         cfg.Listen.Enabled,
		AudioConfigured: cfg.Listen.Configured(),
		URL:             strings.TrimRight(cfg.PublicURL, "/") + "/listen",
	}
}
