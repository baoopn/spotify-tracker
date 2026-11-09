package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/backupstatus"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

// ─── fakes ────────────────────────────────────────────────────────────────────

type fakeBackups struct {
	state    backupstatus.State
	runs     []domain.BackupRun
	inv      []domain.BackupObject
	pending  map[string]*domain.BackupRequest // kind → request
	nextID   int64
	created  []string // "kind by"
	stateErr error
}

func (f *fakeBackups) BackupState(context.Context) (backupstatus.State, error) {
	return f.state, f.stateErr
}

func (f *fakeBackups) BackupRuns(_ context.Context, limit, _ int) ([]domain.BackupRun, int, error) {
	if limit != backupRunsShown {
		return nil, 0, errors.New("unexpected limit")
	}
	return f.runs, len(f.runs), nil
}

func (f *fakeBackups) BackupInventory(context.Context) ([]domain.BackupObject, error) {
	return f.inv, nil
}

func (f *fakeBackups) PendingBackupRequest(_ context.Context, kind string) (*domain.BackupRequest, error) {
	if kind == "" {
		if r := f.pending[domain.BackupRequestBackup]; r != nil {
			return r, nil
		}
		return f.pending[domain.BackupRequestRefresh], nil
	}
	return f.pending[kind], nil
}

func (f *fakeBackups) CreateBackupRequest(_ context.Context, kind, by string) (int64, bool, error) {
	if r := f.pending[kind]; r != nil {
		return r.ID, false, nil
	}
	f.nextID++
	f.pending[kind] = &domain.BackupRequest{ID: f.nextID, Kind: kind, RequestedAt: time.Now()}
	f.created = append(f.created, kind+" "+by)
	return f.nextID, true, nil
}

type fakeBackupSettings struct {
	cur     *settings.Backup
	saved   []settings.Backup
	actor   string
	deletes int
}

func (f *fakeBackupSettings) DeleteBackup(context.Context) error {
	f.deletes++
	f.cur = nil
	return nil
}

func (f *fakeBackupSettings) Backup(context.Context) (settings.Backup, error) {
	if f.cur == nil {
		return settings.Backup{}, settings.ErrNotFound
	}
	return *f.cur, nil
}

func (f *fakeBackupSettings) SaveBackup(_ context.Context, actor string, v settings.Backup) error {
	f.saved = append(f.saved, v)
	f.actor = actor
	f.cur = &v
	return nil
}

// ─── harness ──────────────────────────────────────────────────────────────────

var bkNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

type backupHarness struct {
	db     *fakeBackups
	st     *fakeBackupSettings
	audit  *fakeAuditor
	router chi.Router
}

func strp(s string) *string { return &s }
func intp(n int) *int       { return &n }

func healthyAgent() *domain.BackupAgent {
	next := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	return &domain.BackupAgent{LastSeenAt: bkNow.Add(-30 * time.Second), Version: strp("1.4.0"), Configured: true,
		Destination: strp("s3:my-bucket/spotify-tracker"), Recipients: []string{"age1abc"},
		BackupTime: strp("04:30"), KeepDays: intp(14), KeepMin: intp(5), NextRunAt: &next, SettingsSource: "env"}
}

func newBackupHarness(t *testing.T) *backupHarness {
	t.Helper()
	fin := bkNow.Add(-8*time.Hour + 90*time.Second)
	okRun := domain.BackupRun{ID: 7, Trigger: "schedule", Status: domain.BackupSuccess, StartedAt: bkNow.Add(-8 * time.Hour),
		FinishedAt: &fin, Name: strp("spotify-tracker-20261109T040000Z.dump.age"), SizeBytes: new(int64(1234)),
		SHA256: strp("ab12"), Tables: intp(14)}
	h := &backupHarness{
		db: &fakeBackups{state: backupstatus.State{Agent: healthyAgent(), LastSuccess: &okRun},
			runs:    []domain.BackupRun{okRun, {ID: 8, Trigger: "manual", Status: domain.BackupRunning, StartedAt: bkNow, RequestedBy: strp("a@b.c")}},
			inv:     []domain.BackupObject{{Name: "spotify-tracker-20261109T040000Z.dump.age", SizeBytes: 1234, CreatedAt: bkNow.Add(-8 * time.Hour)}},
			pending: map[string]*domain.BackupRequest{}, nextID: 40},
		st:    &fakeBackupSettings{},
		audit: &fakeAuditor{},
	}
	api := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler)}, backups: h.db, backupSettings: h.st,
		auditor: h.audit, now: func() time.Time { return bkNow }}
	r := chi.NewRouter()
	r.Route("/admin/api", api.mountBackups)
	h.router = r
	return h
}

func (h *backupHarness) do(t *testing.T, method, path, body string) (int, map[string]json.RawMessage) {
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

func dataOf(t *testing.T, body map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var d map[string]json.RawMessage
	if err := json.Unmarshal(body["data"], &d); err != nil {
		t.Fatalf("data: %v", err)
	}
	return d
}

// ─── GET ──────────────────────────────────────────────────────────────────────

func TestGetBackups(t *testing.T) {
	h := newBackupHarness(t)
	h.db.pending[domain.BackupRequestRefresh] = &domain.BackupRequest{ID: 3, Kind: "refresh", RequestedAt: bkNow}
	code, body := h.do(t, http.MethodGet, "/admin/api/backups", "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	d := dataOf(t, body)
	for k, want := range map[string]string{
		"health":          `"ok"`,
		"last_failure":    "null",
		"pending_request": `{"id":3,"kind":"refresh","requested_at":"2026-11-09T12:00:00Z","claimed_at":null}`,
		"settings":        `{"time":"04:30","keep_days":14,"keep_min":5,"paused":false,"source":"default"}`,
		"inventory":       `[{"name":"spotify-tracker-20261109T040000Z.dump.age","size_bytes":1234,"created_at":"2026-11-09T04:00:00Z"}]`,
		"last_success": `{"id":7,"trigger":"schedule","status":"success","started_at":"2026-11-09T04:00:00Z",` +
			`"finished_at":"2026-11-09T04:01:30Z","name":"spotify-tracker-20261109T040000Z.dump.age","size_bytes":1234,` +
			`"sha256":"ab12","tables":14,"error":null,"requested_by":null,"duration_ms":90000}`,
		"agent": `{"last_seen_at":"2026-11-09T11:59:30Z","version":"1.4.0","configured":true,"config_error":null,` +
			`"destination":"s3:my-bucket/spotify-tracker","recipients":["age1abc"],"backup_time":"04:30","keep_days":14,` +
			`"keep_min":5,"paused":false,"next_run_at":"2026-11-10T03:00:00Z","settings_source":"env"}`,
	} {
		if got := string(d[k]); got != want {
			t.Errorf("%s = %s\n want %s", k, got, want)
		}
	}
	var runs []map[string]json.RawMessage
	_ = json.Unmarshal(d["runs"], &runs)
	if len(runs) != 2 || string(runs[1]["duration_ms"]) != "null" || string(runs[1]["requested_by"]) != `"a@b.c"` {
		t.Errorf("runs = %s", d["runs"])
	}
}

func TestGetBackupsNeverRan(t *testing.T) {
	h := newBackupHarness(t)
	h.db.state = backupstatus.State{}
	h.db.runs, h.db.inv = nil, nil
	code, body := h.do(t, http.MethodGet, "/admin/api/backups", "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	d := dataOf(t, body)
	for k, want := range map[string]string{
		"health": `"unknown"`, "health_detail": `"backup service not running"`, "agent": "null",
		"runs": "[]", "inventory": "[]", "pending_request": "null", "last_success": "null",
		"settings": `{"time":"03:00","keep_days":30,"keep_min":7,"paused":false,"source":"default"}`,
	} {
		if got := string(d[k]); got != want {
			t.Errorf("%s = %s, want %s", k, got, want)
		}
	}
}

func TestGetBackupsAdminSettings(t *testing.T) {
	h := newBackupHarness(t)
	h.st.cur = &settings.Backup{Time: "01:15", Paused: true} // keep_* absent: agent values
	h.db.state.Agent.Paused = true
	_, body := h.do(t, http.MethodGet, "/admin/api/backups", "")
	d := dataOf(t, body)
	if got := string(d["settings"]); got != `{"time":"01:15","keep_days":14,"keep_min":5,"paused":true,"source":"admin"}` {
		t.Errorf("settings = %s", got)
	}
	if string(d["health"]) != `"paused"` {
		t.Errorf("health = %s", d["health"])
	}
}

func TestGetBackupsError(t *testing.T) {
	h := newBackupHarness(t)
	h.db.stateErr = errors.New("relation backup_agent does not exist")
	if code, body := h.do(t, http.MethodGet, "/admin/api/backups", ""); code != http.StatusInternalServerError ||
		errCode(t, body) != "internal" {
		t.Errorf("status %d %s", code, errCode(t, body))
	}
}

// ─── run / refresh ────────────────────────────────────────────────────────────

func TestRequestBackup(t *testing.T) {
	for _, tc := range []struct{ path, kind, action string }{
		{"/admin/api/backups/run", "backup", "backups.run_requested"},
		{"/admin/api/backups/refresh", "refresh", "backups.refresh_requested"},
	} {
		h := newBackupHarness(t)
		code, body := h.do(t, http.MethodPost, tc.path, "")
		if code != http.StatusAccepted || string(body["data"]) != `{"id":41}` {
			t.Fatalf("%s: %d %s", tc.path, code, body["data"])
		}
		if len(h.db.created) != 1 || h.db.created[0] != tc.kind+" admin@example.com" {
			t.Errorf("%s: created %v", tc.path, h.db.created)
		}
		if len(h.audit.calls) != 1 || h.audit.calls[0].action != tc.action || h.audit.calls[0].detail["kind"] != tc.kind {
			t.Errorf("%s: audit %+v", tc.path, h.audit.calls)
		}
		code, body = h.do(t, http.MethodPost, tc.path, "")
		if code != http.StatusConflict || errCode(t, body) != "already_pending" {
			t.Errorf("%s again: %d %s", tc.path, code, errCode(t, body))
		}
		if len(h.audit.calls) != 1 {
			t.Errorf("%s: audited a rejected request", tc.path)
		}
	}
}

func TestRequestBackupUnavailable(t *testing.T) {
	for name, mod := range map[string]func(*backupstatus.State){
		"unknown":      func(s *backupstatus.State) { s.Agent = nil },
		"offline":      func(s *backupstatus.State) { s.Agent.LastSeenAt = bkNow.Add(-10 * time.Minute) },
		"unconfigured": func(s *backupstatus.State) { s.Agent.Configured = false; s.Agent.ConfigError = strp("no recipient") },
	} {
		h := newBackupHarness(t)
		mod(&h.db.state)
		code, body := h.do(t, http.MethodPost, "/admin/api/backups/run", "")
		if code != http.StatusServiceUnavailable || errCode(t, body) != "backup_unavailable" {
			t.Errorf("%s: %d %s", name, code, errCode(t, body))
		}
		if len(h.db.created) != 0 {
			t.Errorf("%s: request created", name)
		}
	}
	// Paused or failing: manual runs are still allowed.
	h := newBackupHarness(t)
	h.db.state.Agent.Paused = true
	if code, _ := h.do(t, http.MethodPost, "/admin/api/backups/run", ""); code != http.StatusAccepted {
		t.Errorf("paused: %d", code)
	}
}

// ─── settings ─────────────────────────────────────────────────────────────────

func TestPutBackupSettings(t *testing.T) {
	h := newBackupHarness(t)
	code, body := h.do(t, http.MethodPut, "/admin/api/backups/settings",
		`{"time":" 02:45 ","keep_days":0,"keep_min":5,"paused":true}`)
	if code != http.StatusOK {
		t.Fatalf("status %d %s", code, errCode(t, body))
	}
	if got := string(body["data"]); got != `{"time":"02:45","keep_days":0,"keep_min":5,"paused":true,"source":"admin"}` {
		t.Errorf("data = %s", got)
	}
	if string(body["note"]) != `"The backup service applies new settings within a minute."` {
		t.Errorf("note = %s", body["note"])
	}
	if len(h.st.saved) != 1 || h.st.actor != "admin@example.com" {
		t.Fatalf("saved %+v by %q", h.st.saved, h.st.actor)
	}
	s := h.st.saved[0]
	if s.Time != "02:45" || *s.KeepDays != 0 || *s.KeepMin != 5 || !s.Paused {
		t.Errorf("saved %+v", s)
	}
	raw, _ := json.Marshal(s)
	if string(raw) != `{"time":"02:45","keep_days":0,"keep_min":5,"paused":true}` {
		t.Errorf("stored JSON %s", raw)
	}
	if len(h.audit.calls) != 1 || h.audit.calls[0].action != "backups.settings_updated" {
		t.Fatalf("audit %+v", h.audit.calls)
	}
	diff := h.audit.calls[0].detail
	if _, ok := diff["keep_min"]; ok {
		t.Errorf("unchanged keep_min in diff: %v", diff)
	}
	want := map[string]any{"from": "04:30", "to": "02:45"}
	if got, _ := diff["time"].(map[string]any); got["from"] != want["from"] || got["to"] != want["to"] {
		t.Errorf("time diff %v", diff["time"])
	}
	for _, k := range []string{"keep_days", "paused", "source"} {
		if _, ok := diff[k]; !ok {
			t.Errorf("diff misses %s: %v", k, diff)
		}
	}
}

func TestPutBackupSettingsValidation(t *testing.T) {
	tests := []struct {
		body   string
		fields []string
	}{
		{`{"time":"25:00","keep_days":-1,"keep_min":0,"paused":false}`, []string{"time", "keep_days", "keep_min"}},
		{`{"time":"03:00","keep_days":3651,"keep_min":1001,"paused":false}`, []string{"keep_days", "keep_min"}},
		{`{"time":"03:00","keep_days":30,"keep_min":7}`, []string{"paused"}},
		{`{}`, []string{"time", "keep_days", "keep_min", "paused"}},
	}
	for _, tt := range tests {
		h := newBackupHarness(t)
		code, body := h.do(t, http.MethodPut, "/admin/api/backups/settings", tt.body)
		var e apiError
		_ = json.Unmarshal(body["error"], &e)
		if code != http.StatusUnprocessableEntity || e.Code != "validation_failed" || len(e.Fields) != len(tt.fields) {
			t.Errorf("%s: %d %+v", tt.body, code, e)
		}
		for _, f := range tt.fields {
			if e.Fields[f] == "" {
				t.Errorf("%s: no message for %s", tt.body, f)
			}
		}
		if len(h.st.saved) != 0 || len(h.audit.calls) != 0 {
			t.Errorf("%s: saved or audited", tt.body)
		}
	}
	h := newBackupHarness(t)
	for _, body := range []string{`not json`, `{"time":"03:00","keep_days":1,"keep_min":1,"paused":false,"secret":"x"}`,
		`{"time":3,"keep_days":1,"keep_min":1,"paused":false}`} {
		if code, out := h.do(t, http.MethodPut, "/admin/api/backups/settings", body); code != http.StatusBadRequest ||
			errCode(t, out) != "invalid_request" {
			t.Errorf("%s: %d %s", body, code, errCode(t, out))
		}
	}
}

func TestResetBackupSettings(t *testing.T) {
	h := newBackupHarness(t)
	h.st.cur = &settings.Backup{Time: "01:15", KeepDays: intp(14), KeepMin: intp(5), Paused: true}
	h.db.state.Agent.SettingsSource = "env" // the agent already shows env values
	code, body := h.do(t, http.MethodDelete, "/admin/api/backups/settings", "")
	if code != http.StatusOK {
		t.Fatalf("status %d %s", code, errCode(t, body))
	}
	if got := string(body["data"]); got != `{"time":"04:30","keep_days":14,"keep_min":5,"paused":false,"source":"default"}` {
		t.Errorf("data = %s", got)
	}
	if string(body["note"]) != `"The backup service applies new settings within a minute."` {
		t.Errorf("note = %s", body["note"])
	}
	if h.st.deletes != 1 || h.st.cur != nil {
		t.Errorf("deletes %d", h.st.deletes)
	}
	if len(h.audit.calls) != 1 || h.audit.calls[0].action != "backups.settings_reset" {
		t.Fatalf("audit %+v", h.audit.calls)
	}
	diff := h.audit.calls[0].detail
	for _, k := range []string{"time", "paused", "source"} {
		if _, ok := diff[k]; !ok {
			t.Errorf("diff misses %s: %v", k, diff)
		}
	}
	if _, ok := diff["keep_days"]; ok {
		t.Errorf("unchanged keep_days in diff: %v", diff)
	}
}

func TestResetBackupSettingsAgentStillAdmin(t *testing.T) {
	// Until the agent re-reads the settings, its values are the deleted
	// dashboard ones: show the built-in defaults instead.
	h := newBackupHarness(t)
	h.db.state.Agent.SettingsSource = "admin"
	code, body := h.do(t, http.MethodDelete, "/admin/api/backups/settings", "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got := string(body["data"]); got != `{"time":"03:00","keep_days":30,"keep_min":7,"paused":false,"source":"default"}` {
		t.Errorf("data = %s", got)
	}
}
