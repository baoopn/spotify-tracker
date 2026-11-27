package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/store"
)

type fakeSyncer struct {
	res    listen.SyncResult
	err    error
	status *listen.SyncStatus
	calls  int
}

func (f *fakeSyncer) SyncNow(context.Context) (listen.SyncResult, error) {
	f.calls++
	return f.res, f.err
}

func (f *fakeSyncer) Status(context.Context) (listen.SyncStatus, bool) {
	if f.status == nil {
		return listen.SyncStatus{}, false
	}
	return *f.status, true
}

func (f *fakeSyncer) Interval() time.Duration { return 15 * time.Minute }

type fakeAudioStore struct {
	files   []store.AudioFileRow
	missing []store.MissingAudioRow
	search  string
	page    [2]int
}

func (f *fakeAudioStore) AudioFiles(_ context.Context, q string, limit, offset int) ([]store.AudioFileRow, int64, error) {
	f.search, f.page = q, [2]int{limit, offset}
	return f.files, int64(len(f.files)), nil
}

func (f *fakeAudioStore) AudioSummary(context.Context) (store.AudioSummary, error) {
	return store.AudioSummary{Files: 3, TotalBytes: 3000, TracksInCatalog: 2}, nil
}

func (f *fakeAudioStore) MissingAudio(context.Context, int, int) ([]store.MissingAudioRow, int64, error) {
	return f.missing, int64(len(f.missing)), nil
}

func newAudioHarness(sync AudioSyncer) (*listenersHarness, *fakeAudioStore) {
	h := newListenersHarness()
	st := &fakeAudioStore{files: []store.AudioFileRow{{TrackID: "t1", Ext: "mp3", SizeBytes: 1000}},
		missing: []store.MissingAudioRow{{Track: store.AudioTrack{ID: "t2", Artists: []store.ListenPlayArtist{}}, OwnerPlays: 9}}}
	api := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), AudioSync: sync}, auditor: h.audit, audioStore: st}
	r := chi.NewRouter()
	r.Route("/admin/api", api.mountListenAudio)
	h.router = r
	return h, st
}

func TestAudioFilesRoute(t *testing.T) {
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	h, st := newAudioHarness(&fakeSyncer{status: &listen.SyncStatus{LastSyncAt: &at, Files: 3, LastError: "storage answered 403"}})
	code, out := h.do(t, "GET", "/admin/api/listen/audio?q=song&limit=10&offset=20", ``)
	if code != 200 || st.search != "song" || st.page != [2]int{10, 20} || string(out["total"]) != "1" {
		t.Fatalf("files = %d %s", code, out)
	}
	var sum map[string]any
	_ = json.Unmarshal(out["summary"], &sum)
	if sum["files"] != float64(3) || sum["total_bytes"] != float64(3000) || sum["tracks_in_catalog"] != float64(2) ||
		sum["last_sync_at"] != "2026-11-26T12:00:00Z" || sum["last_error"] != "storage answered 403" || sum["sync_interval_s"] != float64(900) {
		t.Errorf("summary = %v", sum)
	}
	var rows []map[string]any
	_ = json.Unmarshal(out["data"], &rows)
	for _, k := range []string{"track_id", "ext", "size_bytes", "content_type", "last_modified", "first_seen_at", "track", "owner_plays"} {
		if _, ok := rows[0][k]; !ok {
			t.Errorf("missing field %s", k)
		}
	}
	if code, out := h.do(t, "GET", "/admin/api/listen/audio/missing", ``); code != 200 || !strings.Contains(string(out["data"]), `"owner_plays":9`) ||
		!strings.Contains(string(out["data"]), `"last_played_at"`) {
		t.Errorf("missing = %d %s", code, out)
	}
	if code, _ := h.do(t, "GET", "/admin/api/listen/audio?limit=0", ``); code != 400 {
		t.Errorf("bad limit = %d", code)
	}
	// No storage: nulls, interval 0.
	h2, _ := newAudioHarness(nil)
	_, out = h2.do(t, "GET", "/admin/api/listen/audio", ``)
	_ = json.Unmarshal(out["summary"], &sum)
	if sum["last_sync_at"] != nil || sum["last_error"] != nil || sum["sync_interval_s"] != float64(0) {
		t.Errorf("no storage summary = %v", sum)
	}
}

func TestSyncAudioRoute(t *testing.T) {
	sync := &fakeSyncer{res: listen.SyncResult{Files: 5, Added: 2, Removed: 1, TookMS: 42}}
	h, _ := newAudioHarness(sync)
	code, out := h.do(t, "POST", "/admin/api/listen/audio/sync", ``)
	if code != 200 || string(out["files"]) != "5" || string(out["added"]) != "2" || string(out["removed"]) != "1" || string(out["took_ms"]) != "42" {
		t.Fatalf("sync = %d %s", code, out)
	}
	if c := h.audit.calls; len(c) != 1 || c[0].action != "listen.audio_synced" || c[0].detail["files"] != float64(5) || c[0].detail["removed"] != float64(1) {
		t.Errorf("audit = %+v", c)
	}
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{listen.ErrSyncRunning, 409, "sync_running"},
		{listen.ErrSyncCooldown, 429, "rate_limited"},
		{errors.New("audio sync: storage answered 403"), 503, "sync_failed"},
	} {
		sync.err = c.err
		if code, out := h.do(t, "POST", "/admin/api/listen/audio/sync", ``); code != c.status || errCode(t, out) != c.code {
			t.Errorf("%v = %d %s", c.err, code, out)
		}
	}
	if last := h.audit.calls[len(h.audit.calls)-1]; last.detail["error"] != "audio sync: storage answered 403" || len(h.audit.calls) != 2 {
		t.Errorf("failure audit = %+v", h.audit.calls)
	}
	h2, _ := newAudioHarness(nil)
	if code, out := h2.do(t, "POST", "/admin/api/listen/audio/sync", ``); code != 503 || errCode(t, out) != "storage_unconfigured" {
		t.Errorf("no storage = %d %s", code, out)
	}
}
