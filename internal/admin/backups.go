package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/backupstatus"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

// backupStore reads the backup container's status rows and queues requests
// (implemented by *store.Store).
type backupStore interface {
	BackupState(ctx context.Context) (backupstatus.State, error)
	BackupRuns(ctx context.Context, limit, offset int) ([]domain.BackupRun, int, error)
	BackupInventory(ctx context.Context) ([]domain.BackupObject, error)
	PendingBackupRequest(ctx context.Context, kind string) (*domain.BackupRequest, error)
	CreateBackupRequest(ctx context.Context, kind, requestedBy string) (int64, bool, error)
}

// backupSettingsStore loads and saves the dashboard backup settings
// (implemented by *settings.Store).
type backupSettingsStore interface {
	Backup(ctx context.Context) (settings.Backup, error)
	SaveBackup(ctx context.Context, actor string, v settings.Backup) error
	DeleteBackup(ctx context.Context) error
}

// backupRunsShown is how many runs GET /backups returns.
const backupRunsShown = 20

// backupSettingsNote tells the admin when saved settings take effect.
const backupSettingsNote = "The backup service applies new settings within a minute."

func (a *API) mountBackups(r chi.Router) {
	r.Get("/backups", a.getBackups)
	r.Post("/backups/run", a.requestBackup(domain.BackupRequestBackup, "backups.run_requested"))
	r.Post("/backups/refresh", a.requestBackup(domain.BackupRequestRefresh, "backups.refresh_requested"))
	r.Put("/backups/settings", a.putBackupSettings)
	r.Delete("/backups/settings", a.resetBackupSettings)
}

func (a *API) backupDB() backupStore {
	if a.backups != nil {
		return a.backups
	}
	return a.Store
}

func (a *API) backupSettingsDB() backupSettingsStore {
	if a.backupSettings != nil {
		return a.backupSettings
	}
	return a.Settings
}

// ─── views ────────────────────────────────────────────────────────────────────

type backupRunView struct {
	ID          int64      `json:"id"`
	Trigger     string     `json:"trigger"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Name        *string    `json:"name"`
	SizeBytes   *int64     `json:"size_bytes"`
	SHA256      *string    `json:"sha256"`
	Tables      *int       `json:"tables"`
	Error       *string    `json:"error"`
	RequestedBy *string    `json:"requested_by"`
	DurationMS  *int64     `json:"duration_ms"`
}

func toBackupRunView(r *domain.BackupRun) *backupRunView {
	if r == nil {
		return nil
	}
	v := &backupRunView{ID: r.ID, Trigger: r.Trigger, Status: r.Status, StartedAt: r.StartedAt.UTC(),
		FinishedAt: optUTC(r.FinishedAt), Name: r.Name, SizeBytes: r.SizeBytes, SHA256: r.SHA256,
		Tables: r.Tables, Error: r.Error, RequestedBy: r.RequestedBy}
	if r.FinishedAt != nil {
		d := max(r.FinishedAt.Sub(r.StartedAt).Milliseconds(), 0)
		v.DurationMS = &d
	}
	return v
}

type backupObjectView struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type backupAgentView struct {
	LastSeenAt     time.Time  `json:"last_seen_at"`
	Version        *string    `json:"version"`
	Configured     bool       `json:"configured"`
	ConfigError    *string    `json:"config_error"`
	Destination    *string    `json:"destination"`
	Recipients     []string   `json:"recipients"`
	BackupTime     *string    `json:"backup_time"`
	KeepDays       *int       `json:"keep_days"`
	KeepMin        *int       `json:"keep_min"`
	Paused         bool       `json:"paused"`
	NextRunAt      *time.Time `json:"next_run_at"`
	SettingsSource string     `json:"settings_source"`
}

func toBackupAgentView(ag *domain.BackupAgent) *backupAgentView {
	if ag == nil {
		return nil
	}
	rcpt := ag.Recipients
	if rcpt == nil {
		rcpt = []string{}
	}
	return &backupAgentView{LastSeenAt: ag.LastSeenAt.UTC(), Version: ag.Version, Configured: ag.Configured,
		ConfigError: ag.ConfigError, Destination: ag.Destination, Recipients: rcpt, BackupTime: ag.BackupTime,
		KeepDays: ag.KeepDays, KeepMin: ag.KeepMin, Paused: ag.Paused, NextRunAt: optUTC(ag.NextRunAt),
		SettingsSource: ag.SettingsSource}
}

type backupRequestView struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	RequestedAt time.Time  `json:"requested_at"`
	ClaimedAt   *time.Time `json:"claimed_at"`
}

type backupSettingsView struct {
	Time     string `json:"time"`
	KeepDays int    `json:"keep_days"`
	KeepMin  int    `json:"keep_min"`
	Paused   bool   `json:"paused"`
	Source   string `json:"source"` // admin | default
}

// effectiveBackupSettings merges the stored dashboard settings over the
// agent's effective values (which reflect its environment) over the
// built-in defaults.
func effectiveBackupSettings(st *settings.Backup, ag *domain.BackupAgent) backupSettingsView {
	v := backupSettingsView{Time: backupstatus.DefaultTime, KeepDays: backupstatus.DefaultKeepDays,
		KeepMin: backupstatus.DefaultKeepMin, Source: "default"}
	// Without stored settings, agent values that still come from the
	// dashboard (until it re-reads them, within a minute) are not shown.
	if ag != nil && (st != nil || ag.SettingsSource != "admin") {
		if ag.BackupTime != nil && backupstatus.ValidTime(*ag.BackupTime) {
			v.Time = *ag.BackupTime
		}
		if ag.KeepDays != nil {
			v.KeepDays = *ag.KeepDays
		}
		if ag.KeepMin != nil {
			v.KeepMin = *ag.KeepMin
		}
		v.Paused = ag.Paused
	}
	if st != nil {
		v.Source = "admin"
		if st.Time != "" {
			v.Time = st.Time
		}
		if st.KeepDays != nil {
			v.KeepDays = *st.KeepDays
		}
		if st.KeepMin != nil {
			v.KeepMin = *st.KeepMin
		}
		v.Paused = st.Paused
	}
	return v
}

func (a *API) loadBackupSettings(ctx context.Context) (*settings.Backup, error) {
	st, err := a.backupSettingsDB().Backup(ctx)
	if errors.Is(err, settings.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// ─── handlers ─────────────────────────────────────────────────────────────────

// GET /admin/api/backups — status, recent runs, stored backups, settings.
func (a *API) getBackups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := a.backupDB()
	state, err := db.BackupState(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	runs, _, err := db.BackupRuns(ctx, backupRunsShown, 0)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	inv, err := db.BackupInventory(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	pending, err := db.PendingBackupRequest(ctx, "")
	if err != nil {
		a.internal(w, r, err)
		return
	}
	st, err := a.loadBackupSettings(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	health, detail := backupstatus.Compute(state, a.clock())

	runViews := make([]*backupRunView, 0, len(runs))
	for i := range runs {
		runViews = append(runViews, toBackupRunView(&runs[i]))
	}
	invViews := make([]backupObjectView, 0, len(inv))
	for _, o := range inv {
		invViews = append(invViews, backupObjectView{Name: o.Name, SizeBytes: o.SizeBytes, CreatedAt: o.CreatedAt.UTC()})
	}
	var pendingView *backupRequestView
	if pending != nil {
		pendingView = &backupRequestView{ID: pending.ID, Kind: pending.Kind,
			RequestedAt: pending.RequestedAt.UTC(), ClaimedAt: optUTC(pending.ClaimedAt)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"agent":           toBackupAgentView(state.Agent),
		"last_success":    toBackupRunView(state.LastSuccess),
		"last_failure":    toBackupRunView(state.LastFailure),
		"runs":            runViews,
		"inventory":       invViews,
		"pending_request": pendingView,
		"settings":        effectiveBackupSettings(st, state.Agent),
		"health":          health,
		"health_detail":   detail,
	}})
}

// POST /admin/api/backups/run and /backups/refresh — queue a request for
// the backup container, which claims it within a minute.
func (a *API) requestBackup(kind, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		state, err := a.backupDB().BackupState(ctx)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		switch health, detail := backupstatus.Compute(state, a.clock()); health {
		case backupstatus.Unknown, backupstatus.Offline, backupstatus.Unconfigured:
			writeError(w, http.StatusServiceUnavailable, "backup_unavailable",
				"the backup service cannot take requests: "+detail)
			return
		}
		u, _ := UserFrom(ctx)
		id, created, err := a.backupDB().CreateBackupRequest(ctx, kind, u.Email)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		if !created {
			writeError(w, http.StatusConflict, "already_pending", "a "+kind+" request is already queued or running")
			return
		}
		a.audit(r, action, map[string]any{"id": id, "kind": kind})
		writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]int64{"id": id}})
	}
}

// PUT /admin/api/backups/settings {time, keep_days, keep_min, paused}.
func (a *API) putBackupSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Time     *string `json:"time"`
		KeepDays *int    `json:"keep_days"`
		KeepMin  *int    `json:"keep_min"`
		Paused   *bool   `json:"paused"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	fields := map[string]string{}
	if in.Time == nil || in.KeepDays == nil || in.KeepMin == nil {
		for name, missing := range map[string]bool{"time": in.Time == nil, "keep_days": in.KeepDays == nil,
			"keep_min": in.KeepMin == nil} {
			if missing {
				fields[name] = "is required"
			}
		}
	} else {
		*in.Time = strings.TrimSpace(*in.Time)
		fields = backupstatus.Validate(*in.Time, *in.KeepDays, *in.KeepMin)
	}
	if in.Paused == nil {
		fields["paused"] = "is required"
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}

	ctx := r.Context()
	state, err := a.backupDB().BackupState(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	cur, err := a.loadBackupSettings(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	before := effectiveBackupSettings(cur, state.Agent)
	next := settings.Backup{Time: *in.Time, KeepDays: in.KeepDays, KeepMin: in.KeepMin, Paused: *in.Paused}
	u, _ := UserFrom(ctx)
	if err := a.backupSettingsDB().SaveBackup(ctx, u.Email, next); err != nil {
		a.internal(w, r, err)
		return
	}
	after := effectiveBackupSettings(&next, state.Agent)
	a.audit(r, "backups.settings_updated", backupSettingsDiff(before, after))
	writeJSON(w, http.StatusOK, map[string]any{"data": after, "note": backupSettingsNote})
}

// backupSettingsDiff is {field: {from, to}} for every changed value.
func backupSettingsDiff(before, after backupSettingsView) map[string]any {
	diff := map[string]any{}
	add := func(name string, from, to any) {
		if from != to {
			diff[name] = map[string]any{"from": from, "to": to}
		}
	}
	add("time", before.Time, after.Time)
	add("keep_days", before.KeepDays, after.KeepDays)
	add("keep_min", before.KeepMin, after.KeepMin)
	add("paused", before.Paused, after.Paused)
	add("source", before.Source, after.Source)
	return diff
}

// DELETE /admin/api/backups/settings — drop the dashboard settings; the
// backup service goes back to its environment and built-in defaults.
func (a *API) resetBackupSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state, err := a.backupDB().BackupState(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	cur, err := a.loadBackupSettings(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	before := effectiveBackupSettings(cur, state.Agent)
	if err := a.backupSettingsDB().DeleteBackup(ctx); err != nil {
		a.internal(w, r, err)
		return
	}
	after := effectiveBackupSettings(nil, state.Agent)
	a.audit(r, "backups.settings_reset", backupSettingsDiff(before, after))
	writeJSON(w, http.StatusOK, map[string]any{"data": after, "note": backupSettingsNote})
}
