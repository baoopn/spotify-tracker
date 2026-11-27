package listen

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

type fakeAudioDB struct {
	mu   sync.Mutex
	rows map[string]store.TrackAudio
	err  error
	ups  []store.TrackAudio
}

func (f *fakeAudioDB) TrackAudioFor(_ context.Context, id string) (store.TrackAudio, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.TrackAudio{}, f.err
	}
	if a, ok := f.rows[id]; ok {
		return a, nil
	}
	return store.TrackAudio{}, domain.ErrNotFound
}

func (f *fakeAudioDB) UpsertTrackAudio(_ context.Context, a store.TrackAudio) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ups = append(f.ups, a)
	return nil
}

func TestLocateDBFirst(t *testing.T) {
	stub := &s3Stub{def: 404}
	l, _, _ := newTestLocator(t, stub)
	ctype := "audio/flac"
	db := &fakeAudioDB{rows: map[string]store.TrackAudio{trackA: {TrackID: trackA, Ext: "flac", ContentType: &ctype}}}
	l.db = db
	got, err := l.Locate(context.Background(), trackA, 0)
	if err != nil || got == nil || !strings.Contains(got.URL, "/music/audio/"+trackA+".flac?") || got.ContentType != "audio/flac" {
		t.Fatalf("locate = %+v %v", got, err)
	}
	if stub.hits.Load() != 0 {
		t.Errorf("a recorded file sent %d HEAD requests", stub.hits.Load())
	}
}

func TestLocateRecordsHeadFind(t *testing.T) {
	key := "/music/audio/" + trackA + ".m4a"
	stub := &s3Stub{def: 404, status: map[string]int{key: 200}, ctype: map[string]string{key: "audio/mp4"}}
	l, _, _ := newTestLocator(t, stub)
	db := &fakeAudioDB{rows: map[string]store.TrackAudio{}}
	l.db = db
	if got, err := l.Locate(context.Background(), trackA, 0); err != nil || got == nil {
		t.Fatalf("locate = %+v %v", got, err)
	}
	if len(db.ups) != 1 || db.ups[0].TrackID != trackA || db.ups[0].Ext != "m4a" || *db.ups[0].ContentType != "audio/mp4" {
		t.Errorf("upserts = %+v", db.ups)
	}
	// Not found: nothing recorded.
	if got, _ := l.Locate(context.Background(), "0VjIjW4GlUZAMYd2vXMi3b", 0); got != nil || len(db.ups) != 1 {
		t.Errorf("missing = %+v, upserts %d", got, len(db.ups))
	}
}

func TestLocateDBErrorFallsBack(t *testing.T) {
	stub := &s3Stub{def: 200}
	l, _, logs := newTestLocator(t, stub)
	l.db = &fakeAudioDB{err: errors.New("db down")}
	if got, err := l.Locate(context.Background(), trackA, time.Minute); err != nil || got == nil {
		t.Fatalf("locate = %+v %v", got, err)
	}
	if !strings.Contains(logs.String(), "track_audio") {
		t.Errorf("logs = %s", logs)
	}
}
