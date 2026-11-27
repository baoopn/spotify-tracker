package poller

import (
	"testing"
	"time"
)

var anchorT0 = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

// poll feeds one snapshot: the owner seeked to seekTo at changedAt, and the
// poll's midpoint is at mid; raw is what Spotify reported (seekTo + the time
// since the change + jitter).
func poll(a *anchor, track string, changedAt time.Time, playing bool, raw int32, dur int32, mid time.Time) int32 {
	got := a.smooth(track, &changedAt, playing, &raw, dur, mid)
	if got == nil {
		return -1
	}
	return *got
}

func TestAnchorMedianLikeProbe(t *testing.T) {
	var a anchor
	changed := anchorT0
	// The probe: after a seek to 90000, base was 89961..90014 over 9 polls.
	bases := []int64{89961, 90014, 89990, 89975, 90008, 89999, 89983, 90002, 89970}
	var got int32
	for i, b := range bases {
		mid := changed.Add(time.Duration(3*(i+1)) * time.Second)
		elapsed := mid.Sub(changed).Milliseconds()
		got = poll(&a, "t1", changed, true, int32(b+elapsed), 300000, mid)
		if i == 0 && int64(got) != b+elapsed {
			t.Errorf("first poll = %d, want raw %d", got, b+elapsed)
		}
	}
	// Median of the 9 bases is 89990, at 27 s after the seek.
	if want := int32(89990 + 27000); got != want {
		t.Errorf("smoothed = %d, want %d", got, want)
	}
	if len(a.bases) != 9 {
		t.Errorf("samples = %d", len(a.bases))
	}
}

func TestAnchorKeepsLast15(t *testing.T) {
	var a anchor
	for i := range 20 {
		mid := anchorT0.Add(time.Duration(i+1) * time.Second)
		poll(&a, "t1", anchorT0, true, int32(1000+(i+1)*1000+i), 300000, mid) // base drifts by 1 ms per poll
	}
	if len(a.bases) != anchorSamples || a.bases[0] != 1005 {
		t.Errorf("bases = %v", a.bases)
	}
}

func TestAnchorKeyChangeResets(t *testing.T) {
	var a anchor
	for i := range 5 {
		mid := anchorT0.Add(time.Duration(i+1) * time.Second)
		poll(&a, "t1", anchorT0, true, int32(5000+(i+1)*1000), 300000, mid)
	}
	// A seek: new timestamp, new state.
	seek := anchorT0.Add(10 * time.Second)
	if got := poll(&a, "t1", seek, true, 120030, 300000, seek.Add(30*time.Millisecond)); got != 120030 || len(a.bases) != 1 {
		t.Errorf("after seek = %d, samples %d", got, len(a.bases))
	}
	// A skip: new track (base near 0 at the song start).
	skip := anchorT0.Add(20 * time.Second)
	if got := poll(&a, "t2", skip, true, 1005, 200000, skip.Add(time.Second)); got != 1005 || len(a.bases) != 1 {
		t.Errorf("after skip = %d, samples %d", got, len(a.bases))
	}
}

func TestAnchorOutlierResets(t *testing.T) {
	var a anchor
	for i := range 5 {
		mid := anchorT0.Add(time.Duration(i+1) * time.Second)
		poll(&a, "t1", anchorT0, true, int32(10000+(i+1)*1000), 300000, mid)
	}
	// 2 s off with the same timestamp: a state change Spotify did not report.
	mid := anchorT0.Add(6 * time.Second)
	if got := poll(&a, "t1", anchorT0, true, 18000, 300000, mid); got != 18000 || len(a.bases) != 1 {
		t.Errorf("outlier = %d, samples %v", got, a.bases)
	}
	// Small jitter (under 1 s) is smoothed instead.
	mid = mid.Add(time.Second)
	if got := poll(&a, "t1", anchorT0, true, 19300, 300000, mid); got != 19150 {
		t.Errorf("jitter = %d, want 19150", got)
	}
}

func TestAnchorPausedPassesThrough(t *testing.T) {
	var a anchor
	for i := range 3 {
		poll(&a, "t1", anchorT0, true, int32(3000+(i+1)*1000), 300000, anchorT0.Add(time.Duration(i+1)*time.Second))
	}
	pause := anchorT0.Add(4 * time.Second)
	for i := range 3 {
		if got := poll(&a, "t1", pause, false, 7123, 300000, pause.Add(time.Duration(i+1)*time.Second)); got != 7123 {
			t.Errorf("paused = %d", got)
		}
	}
	if len(a.bases) != 0 {
		t.Errorf("paused samples = %v", a.bases)
	}
}

// After a play command Spotify reported is_playing with progress stuck at
// the old song's end and an unchanged timestamp: publish raw, so the
// past-the-end rule makes it idle.
func TestAnchorStuckProgress(t *testing.T) {
	var a anchor
	changed := anchorT0
	poll(&a, "t1", changed, true, 300000, 308400, changed.Add(time.Second))
	for i := range 4 {
		mid := changed.Add(time.Duration(4+3*i) * time.Second)
		if got := poll(&a, "t1", changed, true, 308336, 308400, mid); i > 0 && got != 308336 {
			t.Errorf("stuck poll %d = %d", i, got)
		}
	}
	if len(a.bases) != 0 {
		t.Errorf("stuck samples = %v", a.bases)
	}
}

func TestAnchorClampAndMissing(t *testing.T) {
	var a anchor
	// Clamped to [0, duration].
	if got := poll(&a, "t1", anchorT0, true, 200500, 200000, anchorT0.Add(200*time.Second)); got != 200000 {
		t.Errorf("clamp = %d", got)
	}
	// No timestamp or no progress: passed through, state reset.
	raw := int32(5000)
	if got := a.smooth("t1", nil, true, &raw, 200000, anchorT0); got == nil || *got != 5000 || len(a.bases) != 0 {
		t.Errorf("no timestamp = %v", got)
	}
	ts := anchorT0
	if got := a.smooth("t1", &ts, true, nil, 200000, anchorT0); got != nil {
		t.Errorf("no progress = %v", *got)
	}
}

func TestMedian(t *testing.T) {
	if median([]int64{3, 1, 2}) != 2 || median([]int64{4, 1, 3, 2}) != 2 || median([]int64{7}) != 7 {
		t.Error("median")
	}
	in := []int64{3, 1, 2}
	median(in)
	if in[0] != 3 {
		t.Error("median sorted its input")
	}
}
