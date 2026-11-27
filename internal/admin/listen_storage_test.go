package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/store"
)

type fakeStorage struct {
	s   store.ListenStorage
	err error
}

func (f *fakeStorage) ListenStorageStats(context.Context) (store.ListenStorage, error) {
	return f.s, f.err
}

func TestListenStorage(t *testing.T) {
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	st := &fakeStorage{s: store.ListenStorage{Files: 4, TotalBytes: 4000, TracksPlayed: 10, TracksWithAudio: 3,
		Plays30d: 50, Plays30dWithAudio: 20, Added7d: 2}}
	sync := &fakeSyncer{status: &listen.SyncStatus{LastSyncAt: &at, LastError: "storage answered 403"}}
	a := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), AudioSync: sync}, storage: st}

	raw, _ := json.Marshal(a.listenStorage(context.Background()))
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	want := map[string]any{"files": 4.0, "total_bytes": 4000.0, "tracks_played": 10.0, "tracks_with_audio": 3.0,
		"plays_30d": 50.0, "plays_30d_with_audio": 20.0, "last_sync_at": "2026-11-26T12:00:00Z",
		"last_error": "storage answered 403", "sync_interval_s": 900.0, "added_7d": 2.0}
	if len(got) != len(want) {
		t.Errorf("fields = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}

	// An error never fails the overview: null.
	st.err = errors.New("db down")
	if v := a.listenStorage(context.Background()); v != nil {
		t.Errorf("error = %+v", v)
	}
	// Listen along or S3 off: null, no query.
	a.AudioSync = nil
	st.err = nil
	if v := a.listenStorage(context.Background()); v != nil {
		t.Errorf("off = %+v", v)
	}
	if raw, _ := json.Marshal(a.listenStorage(context.Background())); string(raw) != "null" {
		t.Errorf("off json = %s", raw)
	}
}
