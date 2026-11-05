// Command api serves the public REST API, the admin dashboard and the
// internal gRPC API, and owns the PostgreSQL database.
package main

import (
	// Embedded IANA tz database: zone and DST rules come from the Go
	// toolchain, not from the base image.
	_ "time/tzdata"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/baoopn/spotify-tracker/internal/admin"
	"github.com/baoopn/spotify-tracker/internal/alerts"
	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/artistimages"
	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/grpcapi"
	"github.com/baoopn/spotify-tracker/internal/httpapi"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/notify"
	"github.com/baoopn/spotify-tracker/internal/platform/app"
	"github.com/baoopn/spotify-tracker/internal/platform/ops"
	"github.com/baoopn/spotify-tracker/internal/platform/postgres"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/secrets"
	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
	"github.com/baoopn/spotify-tracker/internal/staticsite"
	"github.com/baoopn/spotify-tracker/internal/store"
	"github.com/baoopn/spotify-tracker/internal/tracker"
	adminui "github.com/baoopn/spotify-tracker/web/admin"
	publicui "github.com/baoopn/spotify-tracker/web/public"
)

func main() { app.Main(run) }

func run(ctx context.Context) error {
	cfg, err := config.Load[config.API]()
	if err != nil {
		return err
	}
	log := logging.New("api", cfg.LogLevel, cfg.LogFormat)
	log.Info("starting", slog.String("version", app.Version), slog.String("env", cfg.Env))

	key, err := secrets.ParseKey(cfg.SecretsKey)
	if err != nil {
		return err
	}
	box, err := secrets.New(key)
	if err != nil {
		return err
	}

	pool, err := postgres.Connect(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cfg.MigrateOnStart {
		if err := postgres.Migrate(ctx, pool, log); err != nil {
			return err
		}
	}
	rdb, err := redisx.Connect(ctx, cfg.Redis)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()

	c, err := wire(ctx, cfg, pool, rdb, box, key, log)
	if err != nil {
		return err
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return c.ops.Run(gctx) })
	g.Go(func() error { return ops.RunHTTP(gctx, c.http, log) })
	g.Go(func() error { return c.alerts.Run(gctx, time.Hour) })
	g.Go(func() error { return c.analytics.Run(gctx, 15*time.Second) })
	g.Go(func() error { return pruneAnalytics(gctx, c.analytics, log) })
	g.Go(func() error { return c.images.Run(gctx) })
	g.Go(func() error { return serveGRPC(gctx, c.grpc, c.health, cfg.GRPCAddr, log) })

	err = g.Wait()
	log.Info("stopped")
	return err
}

// components are the long-running pieces started by run.
type components struct {
	ops       *ops.Server
	http      *http.Server
	grpc      *grpc.Server
	health    *health.Server
	alerts    *alerts.Checker
	analytics *analytics.Recorder
	images    *artistimages.Refresher
}

// wire builds every service and server from config and connections.
func wire(ctx context.Context, cfg config.API, pool *pgxpool.Pool, rdb *redis.Client, box *secrets.Box, key []byte, log *slog.Logger) (*components, error) {
	st := store.New(pool)
	settingsStore := settings.New(pool, box)
	if err := seedSettings(ctx, cfg, settingsStore, log); err != nil {
		return nil, err
	}

	svc := tracker.NewService(st, rdb, log, cfg.ReplayResetThreshold, cfg.EnrichMaxAttempts)
	mgr := spotify.NewManager(&spotify.OAuth{ClientID: cfg.Spotify.ClientID, ClientSecret: cfg.Spotify.ClientSecret},
		settingsStore, rdb, log)
	notifier := notify.New(smtpFromSettings(settingsStore), rdb, log)
	checker := alerts.New(settingsStore, notifier, cfg.Spotify.TokenLifetimeDays,
		strings.TrimRight(cfg.PublicURL, "/")+"/admin/settings/spotify", log)
	mgr.OnInvalidGrant = checker.InvalidGrant
	mgr.OwnerID = cfg.Spotify.OwnerID
	svc.SetPlaylistFetcher(mgr)
	recorder := analytics.NewRecorder(pool, rdb, analyticsSalt(key), log)

	checks := map[string]func(context.Context) error{
		"postgres": st.Ping,
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	}
	opsSrv := ops.NewServer(cfg.OpsAddr, log)
	for name, check := range checks {
		opsSrv.AddCheck(name, check)
	}

	adminAPI := &admin.API{Deps: admin.Deps{
		Store: st, Settings: settingsStore, Spotify: mgr, Tracker: svc, Notifier: notifier,
		Analytics: recorder, Redis: rdb, Log: log.With(slog.String("component", "admin")),
		Version: app.Version, ClerkPublishableKey: cfg.Admin.ClerkPublishableKey,
		RedirectURI: cfg.Spotify.RedirectURI, TokenLifetimeDays: cfg.Spotify.TokenLifetimeDays,
		Checks: checks,
	}}

	grpcSrv, hs := grpcapi.NewServer(svc, mgr, cfg.InternalToken, log, cfg.IsDevelopment())
	return &components{
		ops:       opsSrv,
		http:      newHTTPServer(cfg, st, rdb, recorder, adminAPI, newAuthenticator(cfg, rdb, log), log),
		grpc:      grpcSrv,
		health:    hs,
		alerts:    checker,
		analytics: recorder,
		images:    artistimages.New(st, mgr, log),
	}, nil
}

// smtpFromSettings reads SMTP settings from the database on every send, so
// dashboard edits apply without a restart.
func smtpFromSettings(st *settings.Store) notify.ConfigFunc {
	return func(ctx context.Context) (notify.SMTPConfig, error) {
		e, err := st.Email(ctx)
		if errors.Is(err, settings.ErrNotFound) {
			return notify.SMTPConfig{}, nil
		}
		if err != nil {
			return notify.SMTPConfig{}, err
		}
		return notify.SMTPConfig{Host: e.Host, Port: e.Port, User: e.Username, Password: e.Password, From: e.From, To: e.AlertTo}, nil
	}
}

// newAuthenticator returns the Clerk authenticator, or nil when the admin
// dashboard is not configured.
func newAuthenticator(cfg config.API, rdb *redis.Client, log *slog.Logger) *admin.Authenticator {
	if !cfg.Admin.Enabled() {
		log.Warn("admin dashboard disabled: set CLERK_SECRET_KEY, CLERK_PUBLISHABLE_KEY and ADMIN_ALLOWED_EMAILS")
		return nil
	}
	auth := admin.NewAuthenticator(cfg.Admin.ClerkSecretKey, cfg.Admin.AllowedEmails, cfg.Admin.AllowedUserIDs,
		cfg.Admin.AdminRole, authorizedParties(cfg), rdb, log)
	if cfg.IsDevelopment() {
		auth.AllowLoopbackParties()
	}
	return auth
}

// newHTTPServer builds the public HTTP server: /api/v1, /admin and the
// public site at /.
func newHTTPServer(cfg config.API, st *store.Store, rdb *redis.Client, recorder *analytics.Recorder,
	adminAPI *admin.API, auth *admin.Authenticator, log *slog.Logger) *http.Server {
	return &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(st, rdb, log, httpapi.Options{
			AllowedOrigins:       cfg.CORSAllowedOrigins,
			CacheTTL:             cfg.CacheTTL,
			RateLimitPerMinute:   cfg.RateLimitPerMinute,
			TrustedProxyHops:     cfg.TrustedProxyHops,
			ClientIPHeader:       cfg.ClientIPHeader,
			ShowPrivatePlaylists: cfg.ShowPrivatePlaylists,
			V1Middleware:         []func(http.Handler) http.Handler{recorder.Middleware},
			Mount: func(r chi.Router) {
				admin.Mount(r, adminAPI, auth)
				admin.MountSPA(r, adminui.FS(), cfg.Admin.ClerkPublishableKey)
			},
			Site: staticsite.New(publicui.FS(), staticsite.Options{
				Placeholder: "The public page was not built. Run <code>pnpm --dir web/public build</code>.",
			}),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: /api/v1/now-playing/stream is a long-lived SSE response;
		// regular routes are bounded by a per-route timeout middleware.
	}
}

// pruneAnalytics deletes analytics older than 90 days, once a day.
func pruneAnalytics(ctx context.Context, recorder *analytics.Recorder, log *slog.Logger) error {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := recorder.Prune(ctx, 90*24*time.Hour); err != nil {
				log.Warn("prune analytics failed", logging.Err(err))
			}
		}
	}
}

// serveGRPC serves the internal gRPC API until ctx is done, then stops
// gracefully (forcefully after 10 s).
func serveGRPC(ctx context.Context, srv *grpc.Server, hs *health.Server, addr string, log *slog.Logger) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", addr, err)
	}
	log.Info("grpc server listening", slog.String("addr", ln.Addr().String()))
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	hs.Shutdown()
	stopped := make(chan struct{})
	go func() { srv.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		srv.Stop()
	}
	return nil
}

// seedSettings copies env-provided Spotify / SMTP values into the database
// on first start and whenever the env values change.
func seedSettings(ctx context.Context, cfg config.API, st *settings.Store, log *slog.Logger) error {
	if ok, err := st.SeedSpotify(ctx, cfg.Spotify.RefreshToken, cfg.Spotify.AuthorizedAtTime()); err != nil {
		return fmt.Errorf("seed spotify settings: %w", err)
	} else if ok {
		log.Info("spotify refresh token seeded from environment")
	}
	var to []string
	for _, a := range strings.Split(cfg.Email.AlertTo, ",") {
		if a = strings.TrimSpace(a); a != "" {
			to = append(to, a)
		}
	}
	if ok, err := st.SeedEmail(ctx, settings.Email{
		Host: cfg.Email.SMTPHost, Port: cfg.Email.SMTPPort, Username: cfg.Email.SMTPUser,
		Password: cfg.Email.SMTPPassword, From: cfg.Email.From, AlertTo: to,
	}); err != nil {
		return fmt.Errorf("seed email settings: %w", err)
	} else if ok {
		log.Info("email settings seeded from environment")
	}
	return nil
}

// authorizedParties are the origins Clerk session tokens may come from.
func authorizedParties(cfg config.API) []string {
	var out []string
	if u, err := url.Parse(cfg.PublicURL); err == nil && u.Host != "" {
		out = append(out, u.Scheme+"://"+u.Host)
	}
	return out
}

// analyticsSalt derives a stable, secret salt for hashing client IPs.
func analyticsSalt(key []byte) string {
	sum := sha256.Sum256(append([]byte("spotify-tracker/analytics/"), key...))
	return hex.EncodeToString(sum[:16])
}
