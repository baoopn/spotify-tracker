package tracker

import (
	"fmt"
	"testing"
	"time"
)

func stringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func TestListenStateRoundTrip(t *testing.T) {
	want := Listen{TrackID: "a", StartedAt: t0, ProgressMS: ptr(42_000), ListenedMS: 39_500,
		Counted: true, Replay: true, LastSeen: t0.Add(time.Minute)}
	got := decodeListen(stringMap(encodeListen(want)), t0.Add(time.Hour))
	if got.TrackID != want.TrackID || !got.StartedAt.Equal(want.StartedAt) || *got.ProgressMS != 42_000 ||
		got.ListenedMS != want.ListenedMS || got.Counted != want.Counted || got.Replay != want.Replay ||
		!got.LastSeen.Equal(want.LastSeen) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	noProgress := want
	noProgress.ProgressMS = nil
	if got := decodeListen(stringMap(encodeListen(noProgress)), t0); got.ProgressMS != nil {
		t.Fatalf("progress = %v, want nil", *got.ProgressMS)
	}
}

func TestListenStateEmpty(t *testing.T) {
	if got := decodeListen(map[string]string{}, t0); got.TrackID != "" {
		t.Fatalf("empty hash decoded as %+v", got)
	}
}

// State from before the play-count rules ({track_id, progress_ms}) was for a
// track already written to history: it must not be counted again.
func TestLegacyStateIsCounted(t *testing.T) {
	now := t0.Add(time.Hour)
	l := decodeListen(map[string]string{"track_id": "a", "progress_ms": "90000"}, now)
	if !l.Counted || l.TrackID != "a" || *l.ProgressMS != 90_000 || !l.LastSeen.Equal(now) {
		t.Fatalf("legacy state = %+v", l)
	}
	_, d := Step(l, Snapshot{TrackID: "a", ProgressMS: ptr(93_000), DurationMS: song, IsPlaying: true, At: now.Add(3 * time.Second)}, rules)
	if d != (Decision{}) {
		t.Fatalf("legacy listen counted again: %+v", d)
	}
}
