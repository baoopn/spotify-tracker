package admin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// AudioSyncer lists the audio bucket (implemented by *listen.Syncer).
type AudioSyncer interface {
	SyncNow(ctx context.Context) (listen.SyncResult, error)
	Status(ctx context.Context) (listen.SyncStatus, bool)
	Interval() time.Duration
}

// audioAdminStore reads the recorded audio files (implemented by
// *store.Store).
type audioAdminStore interface {
	AudioFiles(ctx context.Context, search string, limit, offset int) ([]store.AudioFileRow, int64, error)
	AudioSummary(ctx context.Context) (store.AudioSummary, error)
	MissingAudio(ctx context.Context, limit, offset int) ([]store.MissingAudioRow, int64, error)
}

// mountListenAudio registers the Audio files routes. They are never part
// of ReadOnlyHandler.
func (a *API) mountListenAudio(r chi.Router) {
	r.Get("/listen/audio", a.audioFiles)
	r.Get("/listen/audio/missing", a.missingAudio)
	r.Post("/listen/audio/sync", a.syncAudio)
}

func (a *API) audioDB() audioAdminStore {
	if a.audioStore != nil {
		return a.audioStore
	}
	return a.Store
}

// pageParams reads limit (1..100, default 50) and offset; false answers 400.
func pageParams(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit, err := intParam(r, "limit", 50, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return 0, 0, false
	}
	offset, err := intParam(r, "offset", 0, 0, 1_000_000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_offset", err.Error())
		return 0, 0, false
	}
	return limit, offset, true
}

type audioSummaryView struct {
	Files           int64      `json:"files"`
	TotalBytes      int64      `json:"total_bytes"`
	TracksInCatalog int64      `json:"tracks_in_catalog"`
	LastSyncAt      *time.Time `json:"last_sync_at"`
	LastError       *string    `json:"last_error"`
	SyncIntervalS   int64      `json:"sync_interval_s"`
}

func (a *API) audioFiles(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}
	db := a.audioDB()
	rows, total, err := db.AudioFiles(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	sum, err := db.AudioSummary(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	view := audioSummaryView{Files: sum.Files, TotalBytes: sum.TotalBytes, TracksInCatalog: sum.TracksInCatalog}
	if a.AudioSync != nil {
		view.SyncIntervalS = int64(a.AudioSync.Interval() / time.Second)
		if st, ok := a.AudioSync.Status(r.Context()); ok {
			view.LastSyncAt = optUTC(st.LastSyncAt)
			if st.LastError != "" {
				view.LastError = &st.LastError
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows, "total": total, "summary": view})
}

func (a *API) missingAudio(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}
	rows, total, err := a.audioDB().MissingAudio(r.Context(), limit, offset)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows, "total": total})
}

// syncAudio lists the bucket now (up to 60 s) and returns the counts.
func (a *API) syncAudio(w http.ResponseWriter, r *http.Request) {
	if a.AudioSync == nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unconfigured",
			"audio storage is not set up (LISTEN_ENABLED and the S3_* settings)")
		return
	}
	res, err := a.AudioSync.SyncNow(r.Context())
	switch {
	case errors.Is(err, listen.ErrSyncRunning):
		writeError(w, http.StatusConflict, "sync_running", "a sync is already running")
		return
	case errors.Is(err, listen.ErrSyncCooldown):
		writeError(w, http.StatusTooManyRequests, "rate_limited", "wait 30 seconds between syncs")
		return
	case err != nil:
		a.audit(r, "listen.audio_synced", map[string]any{"error": err.Error()})
		a.Log.WarnContext(r.Context(), "audio sync failed", logging.Err(err))
		writeError(w, http.StatusServiceUnavailable, "sync_failed", "the audio bucket could not be listed: "+err.Error())
		return
	}
	a.audit(r, "listen.audio_synced", map[string]any{"files": res.Files, "added": res.Added, "removed": res.Removed})
	writeJSON(w, http.StatusOK, res)
}
