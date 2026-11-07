package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/alerts"
	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/heartbeat"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/notify"
	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
	"github.com/baoopn/spotify-tracker/internal/store"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
	"github.com/baoopn/spotify-tracker/internal/tracker"
)

// Deps are the dependencies of the admin API.
type Deps struct {
	Store     *store.Store
	Settings  *settings.Store
	Spotify   *spotify.Manager
	Tracker   *tracker.Service
	Notifier  *notify.Notifier
	Analytics *analytics.Recorder
	Redis     *redis.Client
	Log       *slog.Logger

	Version             string
	ClerkPublishableKey string
	RedirectURI         string
	TokenLifetimeDays   int
	// Checks are dependency health checks (name → check).
	Checks map[string]func(context.Context) error

	// TopPlaylists runs the Top 50 generator; nil when the feature is disabled.
	TopPlaylists TopPlaylistsAdmin
	// TopPlaylistsConfig is the parsed TOP_PLAYLISTS_* configuration.
	TopPlaylistsConfig topplaylists.Config
}

// API serves /admin/api.
type API struct {
	Deps

	// Test seams; nil uses Deps.
	tops        topPlaylistStore
	spotifyAuth func(context.Context) (settings.Spotify, error)
	auditor     auditor
	invalidate  func(context.Context)
}

// auditor appends audit log entries (implemented by *store.Store).
type auditor interface {
	Audit(ctx context.Context, actor, action string, detail any, ip string) error
}

const (
	stateTTL    = 10 * time.Minute
	statePrefix = "spotify-tracker:admin:oauth-state:"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: msg}})
}

func writeValidation(w http.ResponseWriter, fields map[string]string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]apiError{"error": {
		Code: "validation_failed", Message: "some fields are invalid", Fields: fields,
	}})
}

func (a *API) internal(w http.ResponseWriter, r *http.Request, err error) {
	a.Log.ErrorContext(r.Context(), "admin request failed", slog.String("path", r.URL.Path), logging.Err(err))
	writeError(w, http.StatusInternalServerError, "internal", "internal server error")
}

func clientIP(r *http.Request) string {
	if ip := middleware.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

func (a *API) audit(r *http.Request, action string, detail any) {
	actor := "system"
	if u, ok := UserFrom(r.Context()); ok {
		actor = u.Email
	}
	var aud auditor = a.Store
	if a.auditor != nil {
		aud = a.auditor
	}
	if err := aud.Audit(r.Context(), actor, action, detail, clientIP(r)); err != nil {
		a.Log.WarnContext(r.Context(), "audit write failed", logging.Err(err))
	}
}

func intParam(r *http.Request, name string, def, lo, hi int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, lo, hi)
	}
	return n, nil
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ─── config / auth ────────────────────────────────────────────────────────────

func (a *API) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"clerk_publishable_key": a.ClerkPublishableKey,
		"version":               a.Version,
	})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]User{"user": u})
}

// ─── overview ─────────────────────────────────────────────────────────────────

type service struct {
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	Detail     string     `json:"detail,omitempty"`
}

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	np, err := a.Store.NowPlaying(ctx)
	var nowPlaying *domain.NowPlaying
	switch {
	case err == nil:
		nowPlaying = &np
	case !errors.Is(err, domain.ErrNotFound):
		a.internal(w, r, err)
		return
	}
	all, err := a.Store.Summary(ctx, domain.Range{Name: "all"})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	week, err := a.Store.Summary(ctx, domain.Range{Name: "7d", Since: time.Now().AddDate(0, 0, -7)})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	counts, err := a.Store.EnrichmentCounts(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	sp, err := a.spotifyStatus(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}

	services := []service{{Name: "api", Status: "ok", Detail: "version " + a.Version}}
	services = append(services, a.workerStatus(ctx, "poller", 2*time.Minute), a.workerStatus(ctx, "enricher", 15*time.Minute))
	for _, name := range []string{"postgres", "redis"} {
		s := service{Name: name, Status: "ok"}
		if check, ok := a.Checks[name]; ok {
			if err := check(ctx); err != nil {
				s.Status, s.Detail = "down", err.Error()
			}
		}
		services = append(services, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"now_playing": nowPlaying,
		"summary_all": all,
		"summary_7d":  week,
		"enrichment":  counts,
		"spotify":     sp,
		"services":    services,
		"version":     a.Version,
	})
}

func (a *API) workerStatus(ctx context.Context, name string, staleAfter time.Duration) service {
	hb, err := heartbeat.Read(ctx, a.Redis, name)
	if err != nil {
		return service{Name: name, Status: "down", Detail: "no heartbeat"}
	}
	s := service{Name: name, Status: "ok", LastSeenAt: &hb.LastSeenAt, Detail: hb.Detail}
	switch {
	case time.Since(hb.LastSeenAt) > staleAfter:
		s.Status, s.Detail = "down", "no heartbeat since "+hb.LastSeenAt.Format(time.RFC3339)
	case hb.LastErrorAt != nil && (hb.LastOKAt == nil || hb.LastErrorAt.After(*hb.LastOKAt)):
		s.Status, s.Detail = "degraded", hb.LastError
	}
	return s
}

// ─── spotify ──────────────────────────────────────────────────────────────────

type spotifyStatus struct {
	Connected     bool       `json:"connected"`
	Source        string     `json:"source"`
	AuthorizedAt  *time.Time `json:"authorized_at"`
	ExpiresAt     *time.Time `json:"expires_at"`
	DaysLeft      *int       `json:"days_left"`
	LifetimeDays  int        `json:"lifetime_days"`
	Scopes        []string   `json:"scopes"`
	RedirectURI   string     `json:"redirect_uri"`
	ClientID      string     `json:"client_id"`
	LastRefreshAt *time.Time `json:"last_refresh_at"`
	LastError     *string    `json:"last_error"`
	Account       *struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"account"`
}

func (a *API) spotifyStatus(ctx context.Context) (spotifyStatus, error) {
	st := spotifyStatus{
		LifetimeDays: a.TokenLifetimeDays, RedirectURI: a.RedirectURI,
		ClientID: a.Spotify.OAuth().ClientID, Scopes: []string{},
	}
	sp, err := a.Settings.Spotify(ctx)
	if errors.Is(err, settings.ErrNotFound) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Connected = sp.RefreshToken != ""
	st.Source = sp.Source
	st.AuthorizedAt = sp.AuthorizedAt
	st.LastRefreshAt = sp.LastRefreshAt
	if sp.Scopes != nil {
		st.Scopes = sp.Scopes
	}
	if sp.LastError != "" {
		st.LastError = &sp.LastError
	}
	if sp.AccountID != "" {
		st.Account = &struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		}{sp.AccountID, sp.AccountName}
	}
	if sp.AuthorizedAt != nil {
		exp := alerts.ExpiresAt(*sp.AuthorizedAt, a.TokenLifetimeDays)
		days := int(time.Until(exp).Hours() / 24)
		st.ExpiresAt, st.DaysLeft = &exp, &days
	}
	return st, nil
}

func (a *API) getSpotify(w http.ResponseWriter, r *http.Request) {
	st, err := a.spotifyStatus(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) authorizeSpotify(w http.ResponseWriter, r *http.Request) {
	if a.Spotify.OAuth().ClientID == "" || a.Spotify.OAuth().ClientSecret == "" {
		writeError(w, http.StatusPreconditionFailed, "spotify_not_configured",
			"SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET must be set on the API")
		return
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		a.internal(w, r, err)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	u, _ := UserFrom(r.Context())
	if err := a.Redis.Set(r.Context(), statePrefix+state, u.Email, stateTTL).Err(); err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": a.Spotify.OAuth().AuthorizeURL(state, a.RedirectURI)})
}

// spotifyCallback is reached by the browser redirect from Spotify (no bearer
// token); the single-use state ties it to the admin who started the flow.
func (a *API) spotifyCallback(w http.ResponseWriter, r *http.Request) {
	back := func(result, msg string) {
		q := url.Values{"result": {result}}
		if msg != "" {
			q.Set("message", msg)
		}
		http.Redirect(w, r, "/admin/settings/spotify?"+q.Encode(), http.StatusFound)
	}
	q := r.URL.Query()
	state := q.Get("state")
	if state == "" {
		back("error", "missing state")
		return
	}
	actor, err := a.Redis.GetDel(r.Context(), statePrefix+state).Result()
	if err != nil {
		back("error", "the authorization link expired; start again")
		return
	}
	if e := q.Get("error"); e != "" {
		_ = a.Store.Audit(r.Context(), actor, "spotify.authorize_failed", map[string]string{"error": e}, clientIP(r))
		back("error", "Spotify returned: "+e)
		return
	}
	code := q.Get("code")
	if code == "" {
		back("error", "missing authorization code")
		return
	}
	sp, err := a.Spotify.Connect(r.Context(), actor, code, a.RedirectURI)
	if err != nil {
		a.Log.WarnContext(r.Context(), "spotify authorization failed", logging.Err(err))
		_ = a.Store.Audit(r.Context(), actor, "spotify.authorize_failed", map[string]string{"error": err.Error()}, clientIP(r))
		back("error", err.Error())
		return
	}
	_ = a.Store.Audit(r.Context(), actor, "spotify.authorized",
		map[string]any{"account": sp.AccountName, "scopes": sp.Scopes}, clientIP(r))
	a.Log.InfoContext(r.Context(), "spotify authorized from dashboard", slog.String("account", sp.AccountName))
	back("success", "")
}

func (a *API) testSpotify(w http.ResponseWriter, r *http.Request) {
	player := spotify.NewPlayer(tokenFunc(func(ctx context.Context, force bool) (string, error) {
		tok, _, err := a.Spotify.AccessToken(ctx, force)
		return tok, err
	}), "", nil)
	pb, err := player.CurrentlyPlaying(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	resp := map[string]any{"ok": true, "status": http.StatusNoContent, "now_playing": nil}
	if pb != nil {
		resp["status"] = http.StatusOK
		resp["now_playing"] = domain.NowPlaying{
			IsPlaying: pb.IsPlaying, ProgressMS: pb.ProgressMS, FetchedAt: pb.FetchedAt,
			Track: &domain.TrackView{Track: pb.Track, Album: &pb.Album, Artists: pb.Artists},
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type tokenFunc func(ctx context.Context, force bool) (string, error)

func (f tokenFunc) AccessToken(ctx context.Context, force bool) (string, error) { return f(ctx, force) }

// ─── email settings ───────────────────────────────────────────────────────────

type emailSettings struct {
	Host            string     `json:"host"`
	Port            int        `json:"port"`
	Username        string     `json:"username"`
	From            string     `json:"from"`
	AlertTo         []string   `json:"alert_to"`
	PasswordSet     bool       `json:"password_set"`
	Enabled         bool       `json:"enabled"`
	ExpiryAlertDays []int      `json:"expiry_alert_days"`
	UpdatedAt       *time.Time `json:"updated_at"`
}

func toEmailView(e settings.Email) emailSettings {
	v := emailSettings{
		Host: e.Host, Port: e.Port, Username: e.Username, From: e.From, AlertTo: e.AlertTo,
		PasswordSet: e.Password != "", Enabled: e.Enabled(), ExpiryAlertDays: e.ExpiryAlertDays,
	}
	if !e.UpdatedAt.IsZero() {
		t := e.UpdatedAt
		v.UpdatedAt = &t
	}
	if v.AlertTo == nil {
		v.AlertTo = []string{}
	}
	if v.ExpiryAlertDays == nil {
		v.ExpiryAlertDays = settings.DefaultExpiryAlertDays
	}
	return v
}

func (a *API) loadEmail(ctx context.Context) (settings.Email, error) {
	e, err := a.Settings.Email(ctx)
	if errors.Is(err, settings.ErrNotFound) {
		return settings.Email{Port: 587, AlertTo: []string{}, ExpiryAlertDays: settings.DefaultExpiryAlertDays}, nil
	}
	return e, err
}

func (a *API) getEmail(w http.ResponseWriter, r *http.Request) {
	e, err := a.loadEmail(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toEmailView(e))
}

func (a *API) putEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Host            string   `json:"host"`
		Port            int      `json:"port"`
		Username        string   `json:"username"`
		From            string   `json:"from"`
		AlertTo         []string `json:"alert_to"`
		ExpiryAlertDays []int    `json:"expiry_alert_days"`
		Password        *string  `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	fields := map[string]string{}
	in.Host = strings.TrimSpace(in.Host)
	if in.Host != "" && (strings.ContainsAny(in.Host, " /:") || len(in.Host) > 253) {
		fields["host"] = "must be a hostname"
	}
	if in.Port < 1 || in.Port > 65535 {
		fields["port"] = "must be between 1 and 65535"
	}
	if in.From != "" {
		if _, err := mail.ParseAddress(in.From); err != nil {
			fields["from"] = "must be an email address (optionally with a display name)"
		}
	}
	to := make([]string, 0, len(in.AlertTo))
	for _, addr := range in.AlertTo {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if _, err := mail.ParseAddress(addr); err != nil {
			fields["alert_to"] = "invalid address: " + addr
			break
		}
		to = append(to, addr)
	}
	days := make([]int, 0, len(in.ExpiryAlertDays))
	for _, d := range in.ExpiryAlertDays {
		if d < 1 || d > 90 {
			fields["expiry_alert_days"] = "each value must be between 1 and 90"
			break
		}
		days = append(days, d)
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}

	cur, err := a.loadEmail(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	next := settings.Email{
		Host: in.Host, Port: in.Port, Username: strings.TrimSpace(in.Username), From: strings.TrimSpace(in.From),
		AlertTo: to, ExpiryAlertDays: days, Password: cur.Password,
	}
	if in.Password != nil {
		next.Password = *in.Password
	}
	u, _ := UserFrom(r.Context())
	if err := a.Settings.SaveEmail(r.Context(), u.Email, next); err != nil {
		a.internal(w, r, err)
		return
	}
	a.audit(r, "settings.email.updated", map[string]any{
		"host": next.Host, "port": next.Port, "from": next.From, "alert_to": next.AlertTo,
		"password_changed": in.Password != nil,
	})
	saved, err := a.Settings.Email(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toEmailView(saved))
}

func (a *API) testEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	if r.ContentLength > 0 {
		if err := decode(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
	}
	var to []string
	if in.To = strings.TrimSpace(in.To); in.To != "" {
		if _, err := mail.ParseAddress(in.To); err != nil {
			writeValidation(w, map[string]string{"to": "must be an email address"})
			return
		}
		to = []string{in.To}
	}
	u, _ := UserFrom(r.Context())
	err := a.Notifier.Send(r.Context(), to, "[spotify-tracker] Test email",
		"This is a test email from the spotify-tracker admin dashboard.\n\nSent by "+u.Email+" at "+
			time.Now().UTC().Format(time.RFC1123)+".\n")
	a.audit(r, "settings.email.tested", map[string]any{"to": to, "ok": err == nil})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── data ─────────────────────────────────────────────────────────────────────

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r, "limit", 50, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	var before time.Time
	if c := r.URL.Query().Get("cursor"); c != "" {
		if before, err = time.Parse(time.RFC3339Nano, c); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor must be an RFC 3339 timestamp")
			return
		}
	}
	items, err := a.Store.AdminHistory(r.Context(), limit, before, r.URL.Query().Get("q"))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	resp := map[string]any{"data": items}
	if len(items) == limit {
		resp["next_cursor"] = items[len(items)-1].PlayedAt.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) tracks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := intParam(r, "limit", 25, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	offset, err := intParam(r, "offset", 0, 0, 1_000_000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_offset", err.Error())
		return
	}
	status := q.Get("status")
	switch status {
	case "", domain.EnrichmentPending, domain.EnrichmentEnriched, domain.EnrichmentUnavailable, domain.EnrichmentFailed:
	default:
		writeError(w, http.StatusBadRequest, "invalid_status", "unknown enrichment status")
		return
	}
	rows, total, err := a.Store.AdminTracks(r.Context(), q.Get("q"), status, q.Get("sort"), q.Get("order"), limit, offset)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows, "total": total})
}

func (a *API) track(w http.ResponseWriter, r *http.Request) {
	d, err := a.Store.AdminTrack(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *API) enrichTrack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	isrc, err := a.Store.RequeueTrack(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.Tracker.QueueEnrichment(r.Context(), id, isrc)
	a.Tracker.InvalidateCache(r.Context())
	a.audit(r, "enrichment.requeued", map[string]string{"track_id": id})
	writeJSON(w, http.StatusAccepted, map[string]bool{"queued": true})
}

func (a *API) retryFailed(w http.ResponseWriter, r *http.Request) {
	n, err := a.Store.RequeueFailed(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.Tracker.InvalidateCache(r.Context())
	a.audit(r, "enrichment.requeued", map[string]any{"failed_tracks": n})
	writeJSON(w, http.StatusOK, map[string]int64{"requeued": n})
}

func (a *API) artists(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := intParam(r, "limit", 25, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	offset, err := intParam(r, "offset", 0, 0, 1_000_000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_offset", err.Error())
		return
	}
	rows, total, err := a.Store.AdminArtists(r.Context(), q.Get("q"), q.Get("sort"), q.Get("order"), limit, offset)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows, "total": total})
}

// ─── analytics / audit ────────────────────────────────────────────────────────

func (a *API) analytics(w http.ResponseWriter, r *http.Request) {
	loc, err := analytics.LoadZone(r.URL.Query().Get("tz"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tz", err.Error())
		return
	}
	rep, err := a.Analytics.Query(r.Context(), r.URL.Query().Get("range"), loc)
	if errors.Is(err, analytics.ErrBadRange) {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error())
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (a *API) pageAnalytics(w http.ResponseWriter, r *http.Request) {
	loc, err := analytics.LoadZone(r.URL.Query().Get("tz"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tz", err.Error())
		return
	}
	rep, err := a.Analytics.QueryPages(r.Context(), r.URL.Query().Get("range"), loc)
	if errors.Is(err, analytics.ErrBadRange) {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error())
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (a *API) auditLog(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r, "limit", 50, 1, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	var before int64
	if c := r.URL.Query().Get("cursor"); c != "" {
		if before, err = strconv.ParseInt(c, 10, 64); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "invalid cursor")
			return
		}
	}
	items, err := a.Store.AuditLog(r.Context(), limit, before)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	resp := map[string]any{"data": items}
	if len(items) == limit {
		resp["next_cursor"] = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) playlists(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r, "limit", 25, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	offset, err := intParam(r, "offset", 0, 0, 1_000_000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_offset", err.Error())
		return
	}
	rows, total, err := a.Store.AdminPlaylists(r.Context(), limit, offset)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows, "total": total})
}

func (a *API) refreshPlaylist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "/:?#") {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid playlist id")
		return
	}
	pl, err := a.Tracker.RefreshPlaylist(r.Context(), id)
	if err != nil {
		a.Log.WarnContext(r.Context(), "playlist refresh failed", logging.Err(err))
		writeError(w, http.StatusBadGateway, "spotify_error", err.Error())
		return
	}
	a.audit(r, "playlist.refreshed", map[string]any{"playlist_id": id, "available": pl.Available})
	writeJSON(w, http.StatusOK, pl)
}
