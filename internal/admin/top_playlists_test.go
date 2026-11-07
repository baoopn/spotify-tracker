package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

// ─── fakes ────────────────────────────────────────────────────────────────────

type fakeRunner struct {
	runErr  error
	runs    int
	genRec  domain.TopPlaylistRecord
	genErr  error
	genCall []string // "period start"
}

func (f *fakeRunner) RunNow(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("RunNow without a deadline")
	}
	f.runs++
	return f.runErr
}

func (f *fakeRunner) Generate(ctx context.Context, p topplaylists.Period, start time.Time) (domain.TopPlaylistRecord, error) {
	if _, ok := ctx.Deadline(); !ok {
		return domain.TopPlaylistRecord{}, errors.New("Generate without a deadline")
	}
	f.genCall = append(f.genCall, string(p)+" "+start.UTC().Format(time.RFC3339))
	return f.genRec, f.genErr
}

type fakeTops struct {
	rows       []domain.TopPlaylistRecord
	listArgs   string
	tracks     []domain.TopPlaylistTrack
	hiddenArgs string
}

func (f *fakeTops) find(period string, start time.Time) (domain.TopPlaylistRecord, bool) {
	for _, r := range f.rows {
		if r.Period == period && r.PeriodStart.Equal(start) {
			return r, true
		}
	}
	return domain.TopPlaylistRecord{}, false
}

func (f *fakeTops) AdminTopPlaylists(_ context.Context, period string, limit, offset int) ([]domain.TopPlaylistRecord, int, error) {
	f.listArgs = fmt.Sprintf("%q %d %d", period, limit, offset)
	return f.rows, len(f.rows), nil
}

func (f *fakeTops) TopPlaylist(_ context.Context, period string, start time.Time) (domain.TopPlaylistRecord, error) {
	if r, ok := f.find(period, start); ok {
		return r, nil
	}
	return domain.TopPlaylistRecord{}, domain.ErrNotFound
}

func (f *fakeTops) TopPlaylistTracks(context.Context, string, time.Time) ([]domain.TopPlaylistTrack, error) {
	return f.tracks, nil
}

func (f *fakeTops) SetTopPlaylistHidden(_ context.Context, period string, start time.Time, hidden bool) (domain.TopPlaylistRecord, error) {
	f.hiddenArgs = fmt.Sprintf("%s %s %v", period, start.UTC().Format(time.RFC3339), hidden)
	r, ok := f.find(period, start)
	if !ok {
		return r, domain.ErrNotFound
	}
	r.Hidden = hidden
	return r, nil
}

type auditCall struct {
	actor, action string
	detail        map[string]any
}

type fakeAuditor struct{ calls []auditCall }

func (f *fakeAuditor) Audit(_ context.Context, actor, action string, detail any, _ string) error {
	b, _ := json.Marshal(detail)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	f.calls = append(f.calls, auditCall{actor, action, m})
	return nil
}

// ─── harness ──────────────────────────────────────────────────────────────────

var (
	ny, _     = time.LoadLocation("America/New_York")
	nov2      = time.Date(2026, 11, 2, 0, 0, 0, 0, ny) // after the DST change: -05:00
	published = time.Date(2026, 11, 9, 5, 1, 12, 0, time.UTC)
)

type topHarness struct {
	api    *API
	runner *fakeRunner
	tops   *fakeTops
	audit  *fakeAuditor
	sp     settings.Spotify
	spErr  error
	inval  int
	router chi.Router
}

func createdRow() domain.TopPlaylistRecord {
	next := published.Add(time.Hour)
	return domain.TopPlaylistRecord{Period: "week", PeriodStart: nov2.UTC(), PeriodEnd: nov2.AddDate(0, 0, 7).UTC(),
		TimeZone: "America/New_York", Name: "Top 50 · Week of Nov 2, 2026", Status: domain.TopPlaylistCreated,
		SpotifyPlaylistID: "pl1", SpotifyURL: "https://open.spotify.com/playlist/pl1", TrackCount: 2, TotalPlays: 9,
		Attempts: 1, LastError: "old", NextAttemptAt: &next, PublishedAt: &published,
		CreatedAt: published.Add(-time.Hour), UpdatedAt: published}
}

func pendingRow() domain.TopPlaylistRecord {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, ny)
	return domain.TopPlaylistRecord{Period: "month", PeriodStart: start.UTC(), PeriodEnd: start.AddDate(0, 1, 0).UTC(),
		TimeZone: "America/New_York", Name: "Top 50 · October 2026", Status: domain.TopPlaylistPending,
		CreatedAt: published, UpdatedAt: published}
}

func newTopHarness(t *testing.T, enabled bool) *topHarness {
	t.Helper()
	h := &topHarness{runner: &fakeRunner{}, tops: &fakeTops{rows: []domain.TopPlaylistRecord{createdRow(), pendingRow()}},
		audit: &fakeAuditor{}, sp: settings.Spotify{RefreshToken: "rt", Scopes: []string{spotify.PlaylistWriteScope}}}
	h.api = &API{Deps: Deps{Log: slog.New(slog.DiscardHandler)},
		tops: h.tops, auditor: h.audit, invalidate: func(context.Context) { h.inval++ },
		spotifyAuth: func(context.Context) (settings.Spotify, error) { return h.sp, h.spErr }}
	if enabled {
		h.api.TopPlaylists = h.runner
		h.api.TopPlaylistsConfig = topplaylists.Config{
			Periods: []topplaylists.Period{topplaylists.Week, topplaylists.Month}, Location: ny}
	}
	r := chi.NewRouter()
	r.Route("/admin/api", h.api.mountTopPlaylists)
	h.router = r
	return h
}

func (h *topHarness) do(t *testing.T, method, path, body string) (int, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, User{Email: "admin@example.com"}))
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: invalid JSON %q", method, path, rec.Body)
	}
	return rec.Code, out
}

func errCode(t *testing.T, body map[string]json.RawMessage) string {
	t.Helper()
	var e apiError
	_ = json.Unmarshal(body["error"], &e)
	return e.Code
}

func startPath(r domain.TopPlaylistRecord) string {
	loc, _ := time.LoadLocation(r.TimeZone)
	return "/admin/api/top-playlists/" + r.Period + "/" + url.PathEscape(r.PeriodStart.In(loc).Format(time.RFC3339))
}

// encodeURIComponent-style escaping (":" and "+" escaped too).
func encodeComponent(s string) string {
	return strings.NewReplacer(":", "%3A", "+", "%2B").Replace(s)
}

// ─── list / detail ────────────────────────────────────────────────────────────

func TestTopPlaylistsList(t *testing.T) {
	h := newTopHarness(t, true)
	code, body := h.do(t, http.MethodGet, "/admin/api/top-playlists?period=week&limit=10&offset=5", "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if h.tops.listArgs != `"week" 10 5` {
		t.Errorf("store args = %s", h.tops.listArgs)
	}
	var data []map[string]json.RawMessage
	_ = json.Unmarshal(body["data"], &data)
	if len(data) != 2 || string(body["total"]) != "2" {
		t.Fatalf("data=%d total=%s", len(data), body["total"])
	}
	want := map[string]string{
		"period": `"week"`, "period_start": `"2026-11-02T00:00:00-05:00"`, "period_end": `"2026-11-09T00:00:00-05:00"`,
		"time_zone": `"America/New_York"`, "label": `"Week of Nov 2, 2026"`, "name": `"Top 50 · Week of Nov 2, 2026"`,
		"status": `"created"`, "hidden": "false", "spotify_id": `"pl1"`,
		"spotify_url": `"https://open.spotify.com/playlist/pl1"`, "track_count": "2", "total_plays": "9",
		"attempts": "1", "last_error": `"old"`, "next_attempt_at": `"2026-11-09T06:01:12Z"`,
		"published_at": `"2026-11-09T05:01:12Z"`, "created_at": `"2026-11-09T04:01:12Z"`, "updated_at": `"2026-11-09T05:01:12Z"`,
	}
	for k, v := range want {
		if got := string(data[0][k]); got != v {
			t.Errorf("%s = %s, want %s", k, got, v)
		}
	}
	if len(data[0]) != len(want) {
		t.Errorf("fields = %d, want %d", len(data[0]), len(want))
	}
	for _, k := range []string{"spotify_id", "spotify_url", "last_error", "next_attempt_at", "published_at"} {
		if got := string(data[1][k]); got != "null" {
			t.Errorf("pending %s = %s, want null", k, got)
		}
	}
	if got := string(data[1]["label"]); got != `"October 2026"` {
		t.Errorf("month label = %s", got)
	}
	var st map[string]json.RawMessage
	_ = json.Unmarshal(body["status"], &st)
	wantSt := map[string]string{"enabled": "true", "periods": `["week","month"]`, "time_zone": `"America/New_York"`,
		"authorized": "true", "interval_seconds": "600"}
	for k, v := range wantSt {
		if got := string(st[k]); got != v {
			t.Errorf("status.%s = %s, want %s", k, got, v)
		}
	}
}

func TestTopPlaylistsListDisabledAndEmpty(t *testing.T) {
	h := newTopHarness(t, false)
	h.tops.rows = nil
	h.spErr = settings.ErrNotFound
	code, body := h.do(t, http.MethodGet, "/admin/api/top-playlists", "")
	if code != http.StatusOK || string(body["data"]) != "[]" || string(body["total"]) != "0" {
		t.Fatalf("status %d data=%s total=%s", code, body["data"], body["total"])
	}
	if h.tops.listArgs != `"" 50 0` {
		t.Errorf("defaults = %s", h.tops.listArgs)
	}
	want := `{"enabled":false,"periods":[],"time_zone":"UTC","authorized":false,"interval_seconds":600}`
	if string(body["status"]) != want {
		t.Errorf("status = %s", body["status"])
	}
}

func TestTopPlaylistsAuthorized(t *testing.T) {
	cases := []struct {
		name string
		sp   settings.Spotify
		want string
	}{
		{"unknown scopes", settings.Spotify{RefreshToken: "rt"}, "true"},
		{"missing scope", settings.Spotify{RefreshToken: "rt", Scopes: []string{spotify.RequiredScope}}, "false"},
		{"not connected", settings.Spotify{Scopes: []string{spotify.PlaylistWriteScope}}, "false"},
	}
	for _, c := range cases {
		h := newTopHarness(t, true)
		h.sp = c.sp
		_, body := h.do(t, http.MethodGet, "/admin/api/top-playlists", "")
		var st map[string]json.RawMessage
		_ = json.Unmarshal(body["status"], &st)
		if string(st["authorized"]) != c.want {
			t.Errorf("%s: authorized = %s", c.name, st["authorized"])
		}
	}
}

func TestTopPlaylistsListErrors(t *testing.T) {
	h := newTopHarness(t, true)
	for q, want := range map[string]string{"period=day": "invalid_period", "limit=0": "invalid_limit",
		"limit=101": "invalid_limit", "offset=-1": "invalid_offset"} {
		code, body := h.do(t, http.MethodGet, "/admin/api/top-playlists?"+q, "")
		if code != http.StatusBadRequest || errCode(t, body) != want {
			t.Errorf("%s: %d %s", q, code, errCode(t, body))
		}
	}
}

func TestTopPlaylistDetail(t *testing.T) {
	h := newTopHarness(t, true)
	h.tops.tracks = []domain.TopPlaylistTrack{{Position: 1, Plays: 5,
		Track: domain.TrackView{Track: domain.Track{ID: "t1", Name: "One"}, Artists: []domain.Artist{}}}}
	path := "/admin/api/top-playlists/week/" + encodeComponent("2026-11-02T00:00:00-05:00")
	code, body := h.do(t, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	var d map[string]json.RawMessage
	_ = json.Unmarshal(body["data"], &d)
	if string(d["spotify_id"]) != `"pl1"` || string(d["period_start"]) != `"2026-11-02T00:00:00-05:00"` {
		t.Errorf("data = %s", body["data"])
	}
	var tracks []domain.TopPlaylistTrack
	_ = json.Unmarshal(d["tracks"], &tracks)
	if len(tracks) != 1 || tracks[0].Track.ID != "t1" || tracks[0].Position != 1 {
		t.Errorf("tracks = %s", d["tracks"])
	}

	// The same instant written in UTC addresses the same row; no snapshot is [].
	h.tops.tracks = nil
	code, body = h.do(t, http.MethodGet, "/admin/api/top-playlists/week/2026-11-02T05:00:00Z", "")
	_ = json.Unmarshal(body["data"], &d)
	if code != http.StatusOK || string(d["tracks"]) != "[]" {
		t.Errorf("utc start: %d tracks=%s", code, d["tracks"])
	}

	for path, want := range map[string]struct {
		code int
		err  string
	}{
		"/admin/api/top-playlists/week/2026-11-09T00:00:00Z": {404, "not_found"},
		"/admin/api/top-playlists/week/yesterday":            {400, "invalid_start"},
		"/admin/api/top-playlists/day/2026-11-09T00:00:00Z":  {400, "invalid_period"},
	} {
		code, body := h.do(t, http.MethodGet, path, "")
		if code != want.code || errCode(t, body) != want.err {
			t.Errorf("%s: %d %s", path, code, errCode(t, body))
		}
	}
}

// ─── run / generate / regenerate ──────────────────────────────────────────────

func TestTopPlaylistsRun(t *testing.T) {
	h := newTopHarness(t, true)
	code, body := h.do(t, http.MethodPost, "/admin/api/top-playlists/run", "")
	if code != http.StatusOK || h.runner.runs != 1 || string(body["total"]) != "2" || body["status"] == nil {
		t.Fatalf("status %d runs=%d body=%v", code, h.runner.runs, body)
	}
	if h.tops.listArgs != `"" 50 0` {
		t.Errorf("first page args = %s", h.tops.listArgs)
	}
	if len(h.audit.calls) != 1 || h.audit.calls[0].action != "top_playlists.run" || h.audit.calls[0].actor != "admin@example.com" {
		t.Errorf("audit = %+v", h.audit.calls)
	}

	h.runner.runErr = topplaylists.ErrBusy
	code, body = h.do(t, http.MethodPost, "/admin/api/top-playlists/run", "")
	if code != http.StatusConflict || errCode(t, body) != "busy" || len(h.audit.calls) != 1 {
		t.Errorf("busy: %d %s audits=%d", code, errCode(t, body), len(h.audit.calls))
	}
}

func TestTopPlaylistsMutationsDisabled(t *testing.T) {
	h := newTopHarness(t, false)
	start := encodeComponent("2026-11-02T00:00:00-05:00")
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/admin/api/top-playlists/run", ""},
		{http.MethodPost, "/admin/api/top-playlists/generate", `{"period":"week","date":"2026-11-03"}`},
		{http.MethodPost, "/admin/api/top-playlists/week/" + start + "/regenerate", ""},
		{http.MethodPatch, "/admin/api/top-playlists/week/" + start, `{"hidden":true}`},
	} {
		code, body := h.do(t, c.method, c.path, c.body)
		if code != http.StatusServiceUnavailable || errCode(t, body) != "top_playlists_disabled" {
			t.Errorf("%s %s: %d %s", c.method, c.path, code, errCode(t, body))
		}
	}
	if len(h.audit.calls) != 0 {
		t.Errorf("audited %d", len(h.audit.calls))
	}
}

func TestTopPlaylistGenerate(t *testing.T) {
	h := newTopHarness(t, true)
	h.runner.genRec = pendingRow()
	h.runner.genRec.Status = domain.TopPlaylistCreated
	h.runner.genRec.SpotifyPlaylistID = "pl9"
	code, body := h.do(t, http.MethodPost, "/admin/api/top-playlists/generate", `{"period":"month","date":"2026-10-31"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d %s", code, errCode(t, body))
	}
	// Oct 1 00:00 in New York (EDT, -04:00).
	if len(h.runner.genCall) != 1 || h.runner.genCall[0] != "month 2026-10-01T04:00:00Z" {
		t.Errorf("generate calls = %v", h.runner.genCall)
	}
	var d map[string]json.RawMessage
	_ = json.Unmarshal(body["data"], &d)
	if string(d["status"]) != `"created"` || string(d["spotify_id"]) != `"pl9"` || d["tracks"] != nil {
		t.Errorf("data = %s", body["data"])
	}
	want := map[string]any{"period": "month", "period_start": "2026-10-01T00:00:00-04:00",
		"status": "created", "spotify_id": "pl9"}
	if len(h.audit.calls) != 1 || h.audit.calls[0].action != "top_playlists.generated" ||
		fmt.Sprint(h.audit.calls[0].detail) != fmt.Sprint(want) {
		t.Errorf("audit = %+v", h.audit.calls)
	}
}

func TestTopPlaylistGenerateBadInput(t *testing.T) {
	h := newTopHarness(t, true)
	for body, want := range map[string]string{
		`{`:                                      "invalid_body",
		`{"period":"week","date":"x","extra":1}`: "invalid_body",
		`{"period":"day","date":"2026-10-01"}`:   "invalid_period",
		`{"period":"week","date":"10/01/2026"}`:  "invalid_date",
		`{"period":"week"}`:                      "invalid_date",
	} {
		code, resp := h.do(t, http.MethodPost, "/admin/api/top-playlists/generate", body)
		if code != http.StatusBadRequest || errCode(t, resp) != want {
			t.Errorf("%s: %d %s, want %s", body, code, errCode(t, resp), want)
		}
	}
	if len(h.runner.genCall) != 0 {
		t.Errorf("runner called: %v", h.runner.genCall)
	}
}

func TestTopPlaylistGenerateErrors(t *testing.T) {
	waiting := createdRow()
	waiting.Status, waiting.LastError = domain.TopPlaylistWaitingAuth, "re-authorize Spotify in the admin"
	failed := createdRow()
	failed.Status, failed.LastError = domain.TopPlaylistFailed, "spotify: replace playlist items failed (502)"
	cases := []struct {
		name   string
		rec    domain.TopPlaylistRecord
		err    error
		code   int
		errStr string
		audits int
		msg    string
	}{
		{"busy", domain.TopPlaylistRecord{}, topplaylists.ErrBusy, 409, "busy", 0, ""},
		{"running", domain.TopPlaylistRecord{}, topplaylists.ErrNotCompleted, 400, "period_not_completed", 0, ""},
		{"misaligned", domain.TopPlaylistRecord{}, topplaylists.ErrMisaligned, 400, "invalid_start", 0, ""},
		{"kind disabled", domain.TopPlaylistRecord{}, topplaylists.ErrPeriodDisabled, 400, "invalid_period", 0, ""},
		{"waiting auth", waiting, fmt.Errorf("%w: x: %w", topplaylists.ErrNotAuthorized, spotify.ErrForbidden),
			412, "spotify_not_authorized", 1, "re-authorize Spotify"},
		{"spotify failure", failed, errors.New("spotify: 502"), 502, "spotify_error", 1, "replace playlist items failed (502)"},
		{"internal", domain.TopPlaylistRecord{}, errors.New("db down"), 500, "internal", 0, ""},
	}
	for _, c := range cases {
		h := newTopHarness(t, true)
		h.runner.genRec, h.runner.genErr = c.rec, c.err
		code, body := h.do(t, http.MethodPost, "/admin/api/top-playlists/generate", `{"period":"week","date":"2026-11-03"}`)
		var e apiError
		_ = json.Unmarshal(body["error"], &e)
		if code != c.code || e.Code != c.errStr || len(h.audit.calls) != c.audits || !strings.Contains(e.Message, c.msg) {
			t.Errorf("%s: %d %+v audits=%d", c.name, code, e, len(h.audit.calls))
		}
		if c.audits == 1 && h.audit.calls[0].detail["status"] != c.rec.Status {
			t.Errorf("%s: audit detail = %v", c.name, h.audit.calls[0].detail)
		}
	}
}

func TestTopPlaylistRegenerate(t *testing.T) {
	h := newTopHarness(t, true)
	h.runner.genRec = createdRow()
	path := startPath(createdRow()) + "/regenerate"
	code, body := h.do(t, http.MethodPost, encodeComponentPath(path), "")
	if code != http.StatusOK {
		t.Fatalf("status %d %s", code, errCode(t, body))
	}
	if len(h.runner.genCall) != 1 || h.runner.genCall[0] != "week 2026-11-02T05:00:00Z" {
		t.Errorf("generate calls = %v", h.runner.genCall)
	}
	if len(h.audit.calls) != 1 || h.audit.calls[0].action != "top_playlists.regenerated" ||
		h.audit.calls[0].detail["spotify_id"] != "pl1" || h.audit.calls[0].detail["period_start"] != "2026-11-02T00:00:00-05:00" {
		t.Errorf("audit = %+v", h.audit.calls)
	}

	code, body = h.do(t, http.MethodPost, "/admin/api/top-playlists/week/2026-10-26T04:00:00Z/regenerate", "")
	if code != http.StatusNotFound || errCode(t, body) != "not_found" || len(h.runner.genCall) != 1 {
		t.Errorf("missing row: %d %s", code, errCode(t, body))
	}
}

// encodeComponentPath escapes the start segment like encodeURIComponent.
func encodeComponentPath(p string) string {
	parts := strings.Split(p, "/")
	parts[5] = encodeComponent(parts[5])
	return strings.Join(parts, "/")
}

// ─── hide / show ──────────────────────────────────────────────────────────────

func TestTopPlaylistHide(t *testing.T) {
	h := newTopHarness(t, true)
	path := encodeComponentPath(startPath(createdRow()))
	code, body := h.do(t, http.MethodPatch, path, `{"hidden":true}`)
	if code != http.StatusOK {
		t.Fatalf("status %d %s", code, errCode(t, body))
	}
	var d map[string]json.RawMessage
	_ = json.Unmarshal(body["data"], &d)
	if string(d["hidden"]) != "true" || h.tops.hiddenArgs != "week 2026-11-02T05:00:00Z true" || h.inval != 1 {
		t.Errorf("hidden=%s args=%s inval=%d", d["hidden"], h.tops.hiddenArgs, h.inval)
	}
	if len(h.audit.calls) != 1 || h.audit.calls[0].action != "top_playlists.hidden" ||
		h.audit.calls[0].detail["status"] != "created" {
		t.Errorf("audit = %+v", h.audit.calls)
	}

	code, body = h.do(t, http.MethodPatch, path, `{"hidden":false}`)
	_ = json.Unmarshal(body["data"], &d)
	if code != http.StatusOK || string(d["hidden"]) != "false" || h.inval != 2 ||
		len(h.audit.calls) != 2 || h.audit.calls[1].action != "top_playlists.shown" {
		t.Errorf("show: %d hidden=%s audits=%+v", code, d["hidden"], h.audit.calls)
	}

	for _, bad := range []string{`{}`, `{"hidden":"yes"}`, `{"hidden":true,"x":1}`, ``} {
		code, body := h.do(t, http.MethodPatch, path, bad)
		if code != http.StatusBadRequest || errCode(t, body) != "invalid_body" {
			t.Errorf("%q: %d %s", bad, code, errCode(t, body))
		}
	}
	code, body = h.do(t, http.MethodPatch, "/admin/api/top-playlists/week/2026-10-26T04:00:00Z", `{"hidden":true}`)
	if code != http.StatusNotFound || errCode(t, body) != "not_found" || h.inval != 2 || len(h.audit.calls) != 2 {
		t.Errorf("missing row: %d %s", code, errCode(t, body))
	}
}
