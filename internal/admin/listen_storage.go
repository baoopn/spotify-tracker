package admin

import (
	"context"
	"sync"
	"time"

	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// storageStore reads the storage summary (implemented by *store.Store).
type storageStore interface {
	ListenStorageStats(ctx context.Context) (store.ListenStorage, error)
}

// listenStorageView is the overview's listen_storage.
type listenStorageView struct {
	Files             int64      `json:"files"`
	TotalBytes        int64      `json:"total_bytes"`
	TracksPlayed      int64      `json:"tracks_played"`
	TracksWithAudio   int64      `json:"tracks_with_audio"`
	Plays30d          int64      `json:"plays_30d"`
	Plays30dWithAudio int64      `json:"plays_30d_with_audio"`
	LastSyncAt        *time.Time `json:"last_sync_at"`
	LastError         *string    `json:"last_error"`
	SyncIntervalS     int64      `json:"sync_interval_s"`
	Added7d           int64      `json:"added_7d"`
}

// storageStatsTTL is how long the storage stats are reused: the overview is
// polled every 15 s (and read by MCP), and the query scans the history.
const storageStatsTTL = time.Minute

// storageMemo caches the last storage stats in process.
type storageMemo struct {
	mu sync.Mutex
	at time.Time
	v  store.ListenStorage
}

// storageStats returns the stats, from the memo when younger than a minute.
// Errors are not cached.
func (a *API) storageStats(ctx context.Context) (store.ListenStorage, error) {
	m := &a.storageMemo
	m.mu.Lock()
	defer m.mu.Unlock()
	now := a.clock()
	if !m.at.IsZero() && now.Sub(m.at) < storageStatsTTL {
		return m.v, nil
	}
	v, err := a.storageDB().ListenStorageStats(ctx)
	if err != nil {
		return v, err
	}
	m.v, m.at = v, now
	return v, nil
}

func (a *API) storageDB() storageStore {
	if a.storage != nil {
		return a.storage
	}
	return a.Store
}

// listenStorage is the overview's listen-along storage summary: nil when
// listen along or S3 is off, and nil (logged) on an error, so it never
// fails the overview.
func (a *API) listenStorage(ctx context.Context) *listenStorageView {
	if a.AudioSync == nil {
		return nil
	}
	s, err := a.storageStats(ctx)
	if err != nil {
		a.Log.WarnContext(ctx, "overview: listen storage stats failed", logging.Err(err))
		return nil
	}
	v := &listenStorageView{Files: s.Files, TotalBytes: s.TotalBytes, TracksPlayed: s.TracksPlayed,
		TracksWithAudio: s.TracksWithAudio, Plays30d: s.Plays30d, Plays30dWithAudio: s.Plays30dWithAudio,
		Added7d: s.Added7d, SyncIntervalS: int64(a.AudioSync.Interval() / time.Second)}
	if st, ok := a.AudioSync.Status(ctx); ok {
		v.LastSyncAt = optUTC(st.LastSyncAt)
		if st.LastError != "" {
			v.LastError = &st.LastError
		}
	}
	return v
}
