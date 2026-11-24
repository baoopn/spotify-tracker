package tracker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// fakeTx is an in-memory playbackTx.
type fakeTx struct {
	state    store.ListenState
	lockErr  error
	saveErr  error
	rows     map[string]int32 // "track@played_at" -> replay_count
	order    []string         // row keys, oldest first
	saves    int
	current  string
	byLatest int // replays applied through LatestHistory
}

func newFakeTx() *fakeTx { return &fakeTx{rows: map[string]int32{}} }

func rowKey(track string, at time.Time) string {
	return track + "@" + at.UTC().Format(time.RFC3339Nano)
}

func (f *fakeTx) LockListenState(context.Context) (store.ListenState, error) {
	return f.state, f.lockErr
}

func (f *fakeTx) SaveListenState(_ context.Context, st store.ListenState) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.state = st
	f.saves++
	return nil
}

func (f *fakeTx) SetCurrentPlayback(_ context.Context, trackID string, _ bool, _ *int32, _ time.Time) error {
	f.current = trackID
	return nil
}

func (f *fakeTx) InsertHistory(_ context.Context, trackID string, playedAt, _ time.Time, _ int32, _ int32, _ *domain.PlaybackContext) (bool, error) {
	k := rowKey(trackID, playedAt)
	if _, ok := f.rows[k]; ok {
		return false, nil
	}
	f.rows[k] = 1
	f.order = append(f.order, k)
	return true, nil
}

func (f *fakeTx) IncrementReplayAt(_ context.Context, trackID string, playedAt, _ time.Time) (bool, error) {
	k := rowKey(trackID, playedAt)
	if _, ok := f.rows[k]; !ok {
		return false, nil
	}
	f.rows[k]++
	return true, nil
}

func (f *fakeTx) LatestHistory(context.Context) (string, string, error) {
	if len(f.order) == 0 {
		return "", "", domain.ErrNotFound
	}
	k := f.order[len(f.order)-1]
	for i := range k {
		if k[i] == '@' {
			return k, k[:i], nil
		}
	}
	return k, k, nil
}

func (f *fakeTx) IncrementReplay(_ context.Context, id string, _ time.Time) error {
	f.rows[id]++
	f.byLatest++
	return nil
}

// tx runs one snapshot through recordListen like RecordPlayback does, with
// rollback semantics: on error the fake's state is restored.
func (f *fakeTx) tx(t *testing.T, legacy legacyLoader, track string, progress int32, at, playedAt time.Time) (RecordResult, error) {
	t.Helper()
	saved, rows, order := f.state, cloneRows(f.rows), append([]string(nil), f.order...)
	var res RecordResult
	pb := domain.Playback{Track: domain.Track{ID: track, DurationMS: song}, ProgressMS: ptr(progress),
		IsPlaying: true, PlayedAt: playedAt, FetchedAt: at}
	_, _, err := recordListen(context.Background(), f, legacy, pb, rules, &res)
	if err != nil {
		f.state, f.rows, f.order = saved, rows, order
	}
	return res, err
}

func cloneRows(m map[string]int32) map[string]int32 {
	out := make(map[string]int32, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// listenTo plays track from progress for secs seconds, one snapshot per 3 s.
func (f *fakeTx) listenTo(t *testing.T, now *time.Time, track string, from int32, secs int, playedAt time.Time) {
	t.Helper()
	for i := 0; i <= secs; i += 3 {
		*now = now.Add(3 * time.Second)
		if _, err := f.tx(t, nil, track, from+int32(i)*1000, *now, playedAt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecordListenLoadErrorWritesNothing(t *testing.T) {
	f := newFakeTx()
	now := t0
	f.listenTo(t, &now, "a", 0, 30, t0)
	before := f.state
	f.lockErr = errors.New("db down")
	if _, err := f.tx(t, nil, "a", 34_000, now.Add(3*time.Second), t0); err == nil {
		t.Fatal("lock error was swallowed")
	}
	f.lockErr = nil
	if f.state != before || len(f.rows) != 0 {
		t.Fatalf("state or history changed on a failed load: %+v %v", f.state, f.rows)
	}
	// The outage loses nothing: the listen goes on and counts once.
	f.listenTo(t, &now, "a", 33_000, 40, t0)
	if len(f.rows) != 1 {
		t.Fatalf("rows = %v", f.rows)
	}
}

func TestRecordListenSaveErrorRollsBack(t *testing.T) {
	f := newFakeTx()
	now := t0
	f.listenTo(t, &now, "a", 0, 54, t0)
	f.saveErr = errors.New("db down")
	_, err := f.tx(t, nil, "a", 57_000, now.Add(3*time.Second), t0)
	f.saveErr = nil
	if err == nil || len(f.rows) != 0 {
		t.Fatalf("err = %v, rows = %v", err, f.rows)
	}
	f.listenTo(t, &now, "a", 57_000, 9, t0)
	if len(f.rows) != 1 {
		t.Fatalf("rows = %v, want the play counted once after the retry", f.rows)
	}
}

// ClearPlayback no longer touches the listen: an idle gap (no snapshots)
// followed by a resume continues it.
func TestIdleGapKeepsTheListen(t *testing.T) {
	f := newFakeTx()
	now := t0
	f.listenTo(t, &now, "a", 0, 90, t0)
	now = now.Add(6 * time.Hour)
	f.listenTo(t, &now, "a", 93_000, 60, now)
	if len(f.rows) != 1 || f.rows[rowKey("a", t0)] != 1 {
		t.Fatalf("rows = %v", f.rows)
	}
}

func TestReplayGoesToTheRepeatedRow(t *testing.T) {
	f := newFakeTx()
	now := t0
	f.listenTo(t, &now, "a", 0, 90, t0) // row a@t0
	// Another row lands in between (e.g. an import), so "latest" is not a.
	f.rows[rowKey("z", now)] = 1
	f.order = append(f.order, rowKey("z", now))
	f.listenTo(t, &now, "a", 100_000, 30, t0)
	f.listenTo(t, &now, "a", 0, 90, now) // back to the start: a replay
	if f.rows[rowKey("a", t0)] != 2 || f.byLatest != 0 || f.rows[rowKey("z", t0)] != 0 {
		t.Fatalf("rows = %v (by latest %d)", f.rows, f.byLatest)
	}
}

func TestReplayIsIdempotent(t *testing.T) {
	f := newFakeTx()
	now := t0
	f.listenTo(t, &now, "a", 0, 90, t0)
	f.listenTo(t, &now, "a", 0, 90, now) // replay counted
	at := now
	// The same snapshot delivered again (a retried RPC) changes nothing.
	for i := 0; i < 2; i++ {
		if _, err := f.tx(t, nil, "a", 93_000, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if f.rows[rowKey("a", t0)] != 2 {
		t.Fatalf("rows = %v", f.rows)
	}
}

func TestLegacyStateIsUsedOnceAndCounted(t *testing.T) {
	f := newFakeTx()
	calls := 0
	legacy := func(_ context.Context, now time.Time) Listen {
		calls++
		return decodeLegacyListen(map[string]string{"track_id": "a", "progress_ms": "90000"}, now)
	}
	now := t0
	for i := int32(0); i < 30; i++ {
		now = now.Add(3 * time.Second)
		if _, err := f.tx(t, legacy, "a", 93_000+i*3_000, now, t0.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("legacy state read %d times, want once", calls)
	}
	if len(f.rows) != 0 {
		t.Fatalf("the song playing during the upgrade was counted again: %v", f.rows)
	}
}

func TestDecodeLegacyListen(t *testing.T) {
	if got := decodeLegacyListen(map[string]string{}, t0); got.TrackID != "" {
		t.Fatalf("empty hash decoded as %+v", got)
	}
	l := decodeLegacyListen(map[string]string{"track_id": "a", "progress_ms": "90000"}, t0)
	if !l.Counted || l.TrackID != "a" || *l.ProgressMS != 90_000 || !l.LastSeen.Equal(t0) {
		t.Fatalf("legacy = %+v", l)
	}
	if l := decodeLegacyListen(map[string]string{"track_id": "a", "progress_ms": ""}, t0); l.ProgressMS != nil {
		t.Fatalf("progress = %v, want nil", *l.ProgressMS)
	}
}

func TestListenStateConversionRoundTrip(t *testing.T) {
	want := Listen{TrackID: "a", StartedAt: t0, ProgressMS: ptr(42_000), ListenedMS: 39_500, Counted: true,
		Replay: true, RowAt: t0.Add(-time.Hour), AppliedAt: t0, LastSeen: t0.Add(time.Minute)}
	got := listenFromState(stateFromListen(want))
	if got.TrackID != want.TrackID || !got.StartedAt.Equal(want.StartedAt) || *got.ProgressMS != 42_000 ||
		got.ListenedMS != want.ListenedMS || got.Counted != want.Counted || got.Replay != want.Replay ||
		!got.RowAt.Equal(want.RowAt) || !got.AppliedAt.Equal(want.AppliedAt) || !got.LastSeen.Equal(want.LastSeen) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

// normalizePlayback sets PlayedAt to FetchedAt when Spotify sent no
// timestamp: recordListen must treat it as unknown, not as a seek.
func TestRecordListenWithoutTimestamp(t *testing.T) {
	f := newFakeTx()
	now := t0
	for p := int32(1_000); p < 70_000; p += 3_000 {
		now = now.Add(3 * time.Second)
		if _, err := f.tx(t, nil, "a", p, now, now); err != nil {
			t.Fatal(err)
		}
		if len(f.rows) == 1 {
			if p < 55_000 || p > 61_000 {
				t.Fatalf("counted at %d ms", p)
			}
			return
		}
	}
	t.Fatalf("never counted: listened %d ms", f.state.ListenedMS)
}

func TestOutOfOrderSnapshotKeepsCurrentPlayback(t *testing.T) {
	f := newFakeTx()
	now := t0
	f.listenTo(t, &now, "a", 0, 30, t0)
	before := f.state
	if _, err := f.tx(t, nil, "b", 1_000, now.Add(-time.Second), t0); err != nil {
		t.Fatal(err)
	}
	if f.current != "a" || f.state != before {
		t.Fatalf("an older snapshot moved current_playback to %q or changed the state", f.current)
	}
}
