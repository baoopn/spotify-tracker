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
	s     store.ListenStorage
	err   error
	calls int
}

func (f *fakeStorage) ListenStorageStats(context.Context) (store.ListenStorage, error) {
	f.calls++
	return f.s, f.err
}

func TestListenStorageCached(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	st := &fakeStorage{s: store.ListenStorage{Files: 1}}
	a := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), AudioSync: &fakeSyncer{}}, storage: st,
		now: func() time.Time { return now }}
	a.listenStorage(context.Background())
	now = now.Add(59 * time.Second)
	st.s.Files = 2
	if v := a.listenStorage(context.Background()); v.Files != 1 || st.calls != 1 {
		t.Errorf("within a minute: files %d, calls %d", v.Files, st.calls)
	}
	now = now.Add(2 * time.Second)
	if v := a.listenStorage(context.Background()); v.Files != 2 || st.calls != 2 {
		t.Errorf("after a minute: files %d, calls %d", v.Files, st.calls)
	}
	// Errors are not cached.
	st.err = errors.New("db down")
	now = now.Add(2 * time.Minute)
	a.listenStorage(context.Background())
	st.err = nil
	a.listenStorage(context.Background())
	if st.calls != 4 {
		t.Errorf("error cached: calls %d", st.calls)
	}
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
	a2 := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), AudioSync: sync}, storage: &fakeStorage{err: errors.New("db down")}}
	if v := a2.listenStorage(context.Background()); v != nil {
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
