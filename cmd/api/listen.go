package main

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/admin"
	"github.com/baoopn/spotify-tracker/internal/alerts"
	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/httpapi"
	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/notify"
	"github.com/baoopn/spotify-tracker/internal/staticsite"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// publicSiteOptions configures the public page: its CSP also allows the
// Spotify embed player used for the top playlists, and the live footer credit
// (fetched from baoopn.com, logo served from media.baoopn.com). With listen
// along on, / and /listen also allow Clerk (the account menu signs people in
// on both), and /listen the audio bucket too.
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
		clerkPage := func() map[string]string {
			return map[string]string{
				"script-src":  clerk.Script,
				"connect-src": clerk.Connect,
				"frame-src":   clerk.Frame,
				"img-src":     clerk.Img,
			}
		}
		listenPage := clerkPage()
		if origin := cfg.Listen.Origin(); origin != "" {
			listenPage["media-src"] = origin // audio plays on /listen only
		}
		opts.PageCSP = map[string]map[string]string{"listen.html": listenPage, "index.html": clerkPage()}
	}
	return opts
}

// listenOptions configures /api/v1/listen/*. auth is nil when Clerk is not
// configured (the endpoints then answer 503).
func listenOptions(cfg config.API, st *store.Store, rdb *redis.Client, auth *admin.Authenticator,
	recorder *analytics.Recorder, mail *notify.Notifier, log *slog.Logger) *httpapi.ListenOptions {
	o := &httpapi.ListenOptions{Enabled: cfg.Listen.Enabled, ClerkPublishableKey: cfg.Admin.ClerkPublishableKey}
	if !cfg.Listen.Enabled {
		log.Info("listen along disabled (LISTEN_ENABLED=false)")
		return o
	}
	if st != nil && rdb != nil {
		reqs := listenRequests(cfg, st, rdb, mail, log)
		if cfg.Turnstile.Enabled() {
			reqs.WithTurnstile(listenTurnstile(cfg))
			o.TurnstileSiteKey = strings.TrimSpace(cfg.Turnstile.SiteKey)
		}
		o.Requests = reqs
	}
	log.Info("invite request check", slog.Bool("turnstile", o.TurnstileSiteKey != ""))
	if recorder != nil {
		o.ClientHash = func(ip string) string { return recorder.HashIP("listen-request", ip) }
	}
	if auth != nil {
		o.Auth, o.Me = auth.ListenerMiddleware(st), admin.ListenerMe
		o.Listener = invitedListener
	} else {
		log.Warn("listen along needs the Clerk admin settings; /api/v1/listen answers 503")
	}
	if st != nil {
		o.Plays = st
	}
	if p := listenPresence(cfg, rdb); p != nil {
		o.Presence = p
	}
	// Left nil (not a typed nil) without storage: the audio endpoint then
	// answers storage_unconfigured.
	var db listen.AudioDB
	if st != nil {
		db = st
	}
	if loc := listen.NewLocator(cfg.Listen, rdb, db, log.With(slog.String("component", "listen"))); loc != nil {
		o.Audio = loc
	}
	log.Info("listen along enabled", slog.Bool("audio_configured", cfg.Listen.Configured()),
		slog.String("extensions", strings.Join(cfg.Listen.AudioExtensions, ",")),
		slog.Duration("audio_url_ttl", cfg.Listen.AudioURLTTL), slog.Bool("clerk", auth != nil))
	return o
}

// invitedListener returns the invited listener of a request; an admin
// previewing /listen is not one (nothing is stored for them).
func invitedListener(ctx context.Context) (httpapi.ListenerIdentity, bool) {
	l, ok := admin.ListenerFrom(ctx)
	if !ok || l.Admin || l.ID <= 0 {
		return httpapi.ListenerIdentity{}, false
	}
	return httpapi.ListenerIdentity{ID: l.ID, Email: l.Email}, true
}

// listenPresence is the Redis store of who is on /listen, or nil when
// listen along is off.
func listenPresence(cfg config.API, rdb *redis.Client) *listen.PresenceStore {
	if !cfg.Listen.Enabled || rdb == nil {
		return nil
	}
	return listen.NewPresenceStore(rdb)
}

// attachListen lets the admin show who is listening now and the audio
// files (each left nil, not a typed nil, when off). It returns the bucket
// syncer to run, nil when listen along or the S3 storage is off.
func attachListen(a *admin.API, cfg config.API, st *store.Store, rdb *redis.Client, log *slog.Logger) *listen.Syncer {
	if p := listenPresence(cfg, rdb); p != nil {
		a.Presence = p
	}
	if !cfg.Listen.Enabled || st == nil {
		return nil
	}
	s := listen.NewSyncer(cfg.Listen, st, rdb, log.With(slog.String("component", "listen_sync")))
	if s != nil {
		a.AudioSync = s
		log.Info("audio bucket sync", slog.Duration("interval", cfg.Listen.AudioSyncInterval))
	}
	return s
}

// listenRequests handles "Request an invite": a new request emails the
// alert recipients (when SMTP is set up).
func listenRequests(cfg config.API, st *store.Store, rdb *redis.Client, mail *notify.Notifier, log *slog.Logger) *listen.Requests {
	url := strings.TrimRight(cfg.PublicURL, "/") + "/admin/listeners"
	email := func(r store.ListenRequest) notify.Email {
		return alerts.ListenRequestEmail(r.Name, r.Email, strOr(r.Message), strOr(r.Country), r.CreatedAt, url)
	}
	rlog := log.With(slog.String("component", "listen_requests"))
	if mail == nil {
		return listen.NewRequests(st, rdb, nil, email, rlog) // an untyped nil: no alerts
	}
	return listen.NewRequests(st, rdb, mail, email, rlog)
}

// listenTurnstile verifies Turnstile tokens. Outside development the reply's
// hostname must be the PUBLIC_URL host.
func listenTurnstile(cfg config.API) *listen.Turnstile {
	host := ""
	if u, err := url.Parse(cfg.PublicURL); err == nil && !cfg.IsDevelopment() {
		host = u.Hostname()
	}
	return listen.NewTurnstile(strings.TrimSpace(cfg.Turnstile.SecretKey), host)
}

func strOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// pruneListenRequests deletes invite requests decided more than 90 days ago
// (run daily with the analytics prune).
func pruneListenRequests(st *store.Store, log *slog.Logger) func(context.Context) {
	return func(ctx context.Context) {
		if st == nil {
			return
		}
		if n, err := st.PruneListenRequests(ctx, 90*24*time.Hour); err != nil {
			log.Warn("prune listen requests failed", logging.Err(err))
		} else if n > 0 {
			log.Info("pruned listen requests", slog.Int64("deleted", n))
		}
	}
}

// listenInfo describes listen along for the admin Listeners page.
func listenInfo(cfg config.API) admin.ListenInfo {
	return admin.ListenInfo{
		Enabled:         cfg.Listen.Enabled,
		AudioConfigured: cfg.Listen.Configured(),
		URL:             strings.TrimRight(cfg.PublicURL, "/") + "/listen",
	}
}
