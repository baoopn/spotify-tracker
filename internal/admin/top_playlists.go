package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

// TopPlaylistsAdmin runs the Top 50 generator on demand (implemented by
// *topplaylists.Runner).
type TopPlaylistsAdmin interface {
	RunNow(ctx context.Context) error
	Generate(ctx context.Context, p topplaylists.Period, start time.Time) (domain.TopPlaylistRecord, error)
	// Regenerate is Generate for an existing row; on the running period of a
	// live kind it forces a live refresh.
	Regenerate(ctx context.Context, p topplaylists.Period, start time.Time) (domain.TopPlaylistRecord, error)
}

// topPlaylistStore reads and updates top playlist rows (implemented by *store.Store).
type topPlaylistStore interface {
	AdminTopPlaylists(ctx context.Context, period string, limit, offset int) ([]domain.TopPlaylistRecord, int, error)
	TopPlaylist(ctx context.Context, period string, start time.Time) (domain.TopPlaylistRecord, error)
	TopPlaylistTracks(ctx context.Context, period string, start time.Time) ([]domain.TopPlaylistTrack, error)
	SetTopPlaylistHidden(ctx context.Context, period string, start time.Time, hidden bool) (domain.TopPlaylistRecord, error)
}

// topPlaylistsTimeout bounds run / generate / regenerate (the admin router
// times out after 30 s).
const topPlaylistsTimeout = 25 * time.Second

func (a *API) mountTopPlaylists(r chi.Router) {
	r.Get("/top-playlists", a.listTopPlaylists)
	r.Post("/top-playlists/run", a.runTopPlaylists)
	r.Post("/top-playlists/generate", a.generateTopPlaylist)
	r.Get("/top-playlists/{period}/{start}", a.topPlaylist)
	r.Patch("/top-playlists/{period}/{start}", a.setTopPlaylistHidden)
	r.Post("/top-playlists/{period}/{start}/regenerate", a.regenerateTopPlaylist)
}

// ─── seams (overridden in tests) ──────────────────────────────────────────────

func (a *API) topStore() topPlaylistStore {
	if a.tops != nil {
		return a.tops
	}
	return a.Store
}

func (a *API) spotifySettings(ctx context.Context) (settings.Spotify, error) {
	if a.spotifyAuth != nil {
		return a.spotifyAuth(ctx)
	}
	return a.Settings.Spotify(ctx)
}

func (a *API) invalidatePublic(ctx context.Context) {
	if a.invalidate != nil {
		a.invalidate(ctx)
		return
	}
	a.Tracker.InvalidateCache(ctx)
}

// ─── views ────────────────────────────────────────────────────────────────────

type adminTopPlaylist struct {
	Period        string     `json:"period"`
	PeriodStart   time.Time  `json:"period_start"` // in the row's zone
	PeriodEnd     time.Time  `json:"period_end"`
	TimeZone      string     `json:"time_zone"`
	Label         string     `json:"label"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	Hidden        bool       `json:"hidden"`
	Live          bool       `json:"live"` // live playlist of a period that is still running
	RefreshedAt   *time.Time `json:"refreshed_at"`
	SpotifyID     *string    `json:"spotify_id"`
	SpotifyURL    *string    `json:"spotify_url"`
	TrackCount    int        `json:"track_count"`
	TotalPlays    int64      `json:"total_plays"`
	Attempts      int        `json:"attempts"`
	LastError     *string    `json:"last_error"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	PublishedAt   *time.Time `json:"published_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type adminTopPlaylistDetail struct {
	adminTopPlaylist
	Tracks []domain.TopPlaylistTrack `json:"tracks"`
}

type topPlaylistsStatus struct {
	Enabled         bool     `json:"enabled"`
	Periods         []string `json:"periods"`
	TimeZone        string   `json:"time_zone"`
	Authorized      bool     `json:"authorized"`
	IntervalSeconds int      `json:"interval_seconds"`
	// LivePeriods lists the kinds whose current period has a live playlist.
	LivePeriods        []string `json:"live_periods"`
	LiveRefreshSeconds int      `json:"live_refresh_seconds"`
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func (a *API) topZone() *time.Location {
	if loc := a.TopPlaylistsConfig.Location; loc != nil && a.TopPlaylists != nil {
		return loc
	}
	return time.UTC
}

func (a *API) toAdminTopPlaylist(row domain.TopPlaylistRecord) adminTopPlaylist {
	zone := a.topZone()
	if row.TimeZone != "" {
		if l, err := time.LoadLocation(row.TimeZone); err == nil {
			zone = l // the zone the period was generated in
		}
	}
	start := row.PeriodStart.In(zone)
	return adminTopPlaylist{
		Period: row.Period, PeriodStart: start, PeriodEnd: row.PeriodEnd.In(zone), TimeZone: zone.String(),
		Label: topplaylists.Label(topplaylists.Period(row.Period), start), Name: row.Name,
		Status: row.Status, Hidden: row.Hidden,
		// An ended period whose live playlist is not finalized yet is finished.
		Live: row.Live && a.clock().Before(row.PeriodEnd), RefreshedAt: optUTC(row.RefreshedAt),
		SpotifyID: optString(row.SpotifyPlaylistID), SpotifyURL: optString(row.SpotifyURL),
		TrackCount: row.TrackCount, TotalPlays: row.TotalPlays, Attempts: row.Attempts,
		LastError: optString(row.LastError), NextAttemptAt: optUTC(row.NextAttemptAt),
		PublishedAt: optUTC(row.PublishedAt), CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
}

func (a *API) topPlaylistsStatus(ctx context.Context) (topPlaylistsStatus, error) {
	st := topPlaylistsStatus{Periods: []string{}, TimeZone: a.topZone().String(),
		IntervalSeconds: int(topplaylists.DefaultInterval / time.Second), LivePeriods: []string{},
		LiveRefreshSeconds: int(a.TopPlaylistsConfig.Refresh() / time.Second)}
	if a.TopPlaylists != nil && a.TopPlaylistsConfig.Enabled() {
		st.Enabled = true
		for _, p := range a.TopPlaylistsConfig.Periods {
			st.Periods = append(st.Periods, string(p))
			if a.TopPlaylistsConfig.IsLive(p) {
				st.LivePeriods = append(st.LivePeriods, string(p))
			}
		}
	}
	sp, err := a.spotifySettings(ctx)
	switch {
	case errors.Is(err, settings.ErrNotFound):
		return st, nil
	case err != nil:
		return st, err
	}
	// Unknown scopes (a token seeded from the environment) are given a try,
	// like the generator does.
	st.Authorized = sp.RefreshToken != "" &&
		(len(sp.Scopes) == 0 || spotify.HasScope(sp.Scopes, spotify.PlaylistWriteScope))
	return st, nil
}

// ─── handlers ─────────────────────────────────────────────────────────────────

func validPeriod(s string) bool {
	switch topplaylists.Period(s) {
	case topplaylists.Week, topplaylists.Month, topplaylists.Year:
		return true
	}
	return false
}

// pathPeriod parses {period} and {start} (RFC 3339, URL-encoded by the client).
func pathPeriod(w http.ResponseWriter, r *http.Request) (topplaylists.Period, time.Time, bool) {
	p := chi.URLParam(r, "period")
	if !validPeriod(p) {
		writeError(w, http.StatusBadRequest, "invalid_period", "period must be week, month or year")
		return "", time.Time{}, false
	}
	raw, err := url.PathUnescape(chi.URLParam(r, "start"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_start", "start must be an RFC 3339 timestamp")
		return "", time.Time{}, false
	}
	start, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_start", "start must be an RFC 3339 timestamp")
		return "", time.Time{}, false
	}
	return topplaylists.Period(p), start, true
}

func (a *API) requireTopPlaylists(w http.ResponseWriter) bool {
	if a.TopPlaylists == nil {
		writeError(w, http.StatusServiceUnavailable, "top_playlists_disabled",
			"top playlists are disabled (set TOP_PLAYLISTS_PERIODS)")
		return false
	}
	return true
}

// writeTopPlaylistPage writes { data, total, status }.
func (a *API) writeTopPlaylistPage(w http.ResponseWriter, r *http.Request, period string, limit, offset int) {
	rows, total, err := a.topStore().AdminTopPlaylists(r.Context(), period, limit, offset)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	status, err := a.topPlaylistsStatus(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	data := make([]adminTopPlaylist, 0, len(rows))
	for _, row := range rows {
		data = append(data, a.toAdminTopPlaylist(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "total": total, "status": status})
}

// GET /admin/api/top-playlists?period&limit&offset — every row, all statuses.
func (a *API) listTopPlaylists(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period != "" && !validPeriod(period) {
		writeError(w, http.StatusBadRequest, "invalid_period", "period must be week, month or year")
		return
	}
	limit, err := intParam(r, "limit", 50, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	offset, err := intParam(r, "offset", 0, 0, 1_000_000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_offset", err.Error())
		return
	}
	a.writeTopPlaylistPage(w, r, period, limit, offset)
}

// GET /admin/api/top-playlists/{period}/{start} — one row with its snapshot.
func (a *API) topPlaylist(w http.ResponseWriter, r *http.Request) {
	p, start, ok := pathPeriod(w, r)
	if !ok {
		return
	}
	row, err := a.topStore().TopPlaylist(r.Context(), string(p), start)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "top playlist not found")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	tracks, err := a.topStore().TopPlaylistTracks(r.Context(), row.Period, row.PeriodStart)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if tracks == nil {
		tracks = []domain.TopPlaylistTrack{}
	}
	writeJSON(w, http.StatusOK, map[string]adminTopPlaylistDetail{
		"data": {adminTopPlaylist: a.toAdminTopPlaylist(row), Tracks: tracks},
	})
}

// POST /admin/api/top-playlists/run — one generator tick now.
func (a *API) runTopPlaylists(w http.ResponseWriter, r *http.Request) {
	if !a.requireTopPlaylists(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), topPlaylistsTimeout)
	defer cancel()
	err := a.TopPlaylists.RunNow(ctx)
	if errors.Is(err, topplaylists.ErrBusy) {
		writeError(w, http.StatusConflict, "busy", "top playlists are being generated; try again shortly")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	periods := make([]string, len(a.TopPlaylistsConfig.Periods))
	for i, p := range a.TopPlaylistsConfig.Periods {
		periods[i] = string(p)
	}
	a.audit(r, "top_playlists.run", map[string]any{"periods": periods})
	a.writeTopPlaylistPage(w, r, "", 50, 0)
}

// POST /admin/api/top-playlists/generate {period, date} — backfill or
// refresh the completed period containing date (in the configured zone).
func (a *API) generateTopPlaylist(w http.ResponseWriter, r *http.Request) {
	if !a.requireTopPlaylists(w) {
		return
	}
	var in struct {
		Period string `json:"period"`
		Date   string `json:"date"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body: "+err.Error())
		return
	}
	if !validPeriod(in.Period) {
		writeError(w, http.StatusBadRequest, "invalid_period", "period must be week, month or year")
		return
	}
	loc := a.topZone()
	day, err := time.ParseInLocation("2006-01-02", in.Date, loc)
	if err != nil || day.Year() < 2000 {
		writeError(w, http.StatusBadRequest, "invalid_date", "date must be a calendar date (YYYY-MM-DD) from 2000 on")
		return
	}
	p := topplaylists.Period(in.Period)
	start, _ := topplaylists.Bounds(p, day, loc)
	a.runGenerate(w, r, "top_playlists.generated", p, start, a.TopPlaylists.Generate)
}

// POST /admin/api/top-playlists/{period}/{start}/regenerate — forced
// re-generation of an existing row into the same Spotify playlist: a live
// row is refreshed now, a live row of an ended period is finalized.
func (a *API) regenerateTopPlaylist(w http.ResponseWriter, r *http.Request) {
	if !a.requireTopPlaylists(w) {
		return
	}
	p, start, ok := pathPeriod(w, r)
	if !ok {
		return
	}
	row, err := a.topStore().TopPlaylist(r.Context(), string(p), start)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "top playlist not found")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.runGenerate(w, r, "top_playlists.regenerated", p, row.PeriodStart, a.TopPlaylists.Regenerate)
}

type generateFunc func(ctx context.Context, p topplaylists.Period, start time.Time) (domain.TopPlaylistRecord, error)

func (a *API) runGenerate(w http.ResponseWriter, r *http.Request, action string, p topplaylists.Period, start time.Time,
	generate generateFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), topPlaylistsTimeout)
	defer cancel()
	rec, err := generate(ctx, p, start)
	switch {
	case errors.Is(err, topplaylists.ErrBusy):
		writeError(w, http.StatusConflict, "busy", "top playlists are being generated; try again shortly")
		return
	case errors.Is(err, topplaylists.ErrMisaligned):
		writeError(w, http.StatusBadRequest, "invalid_start", "start is not the beginning of a "+string(p))
		return
	case errors.Is(err, topplaylists.ErrNotCompleted):
		writeError(w, http.StatusBadRequest, "period_not_completed", "the "+string(p)+" has not ended yet")
		return
	case errors.Is(err, topplaylists.ErrPeriodDisabled):
		writeError(w, http.StatusBadRequest, "invalid_period", "top playlists are not generated for this period kind")
		return
	}
	if rec.Period != "" { // an attempt was made and stored
		view := a.toAdminTopPlaylist(rec)
		a.audit(r, action, map[string]any{
			"period": rec.Period, "period_start": view.PeriodStart.Format(time.RFC3339),
			"status": rec.Status, "spotify_id": rec.SpotifyPlaylistID,
		})
	}
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]adminTopPlaylist{"data": a.toAdminTopPlaylist(rec)})
	case errors.Is(err, topplaylists.ErrNoPlays):
		writeError(w, http.StatusConflict, "no_plays", "no plays in this period; playlist left unchanged")
	case errors.Is(err, topplaylists.ErrNotAuthorized):
		msg := err.Error()
		if rec.Status == domain.TopPlaylistWaitingAuth && rec.LastError != "" {
			msg = rec.LastError
		}
		writeError(w, http.StatusPreconditionFailed, "spotify_not_authorized", msg)
	case rec.LastError != "" && (rec.Status == domain.TopPlaylistFailed || rec.Status == domain.TopPlaylistCreated):
		// failed, or a published row whose regeneration failed (kept public)
		writeError(w, http.StatusBadGateway, "spotify_error", "generation failed: "+rec.LastError)
	default:
		a.internal(w, r, err)
	}
}

// PATCH /admin/api/top-playlists/{period}/{start} {hidden} — show or hide
// the playlist on the public page.
func (a *API) setTopPlaylistHidden(w http.ResponseWriter, r *http.Request) {
	if !a.requireTopPlaylists(w) {
		return
	}
	p, start, ok := pathPeriod(w, r)
	if !ok {
		return
	}
	var in struct {
		Hidden *bool `json:"hidden"`
	}
	if err := decode(r, &in); err != nil || in.Hidden == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", `body must be {"hidden": true|false}`)
		return
	}
	row, err := a.topStore().SetTopPlaylistHidden(r.Context(), string(p), start, *in.Hidden)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "top playlist not found")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.invalidatePublic(r.Context())
	view := a.toAdminTopPlaylist(row)
	action := "top_playlists.shown"
	if row.Hidden {
		action = "top_playlists.hidden"
	}
	a.audit(r, action, map[string]any{
		"period": row.Period, "period_start": view.PeriodStart.Format(time.RFC3339),
		"status": row.Status, "spotify_id": row.SpotifyPlaylistID,
	})
	writeJSON(w, http.StatusOK, map[string]adminTopPlaylist{"data": view})
}
