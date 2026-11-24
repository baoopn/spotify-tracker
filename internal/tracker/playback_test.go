package tracker

import (
	"testing"
	"time"
)

func ptr(v int32) *int32 { return &v }

var (
	t0    = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	rules = PlayRules{MinFraction: 0.3, ResumeWindow: 24 * time.Hour, ReplayThreshold: 10 * time.Second}
)

// player feeds snapshots through Step and records the decisions.
type player struct {
	t       *testing.T
	listen  Listen
	now     time.Time
	plays   []time.Time // played_at of new history rows
	replays int
}

func newPlayer(t *testing.T) *player { return &player{t: t, now: t0} }

// snap sends one snapshot `after` the previous one.
func (p *player) snap(after time.Duration, track string, progress *int32, duration int32, playing bool, playedAt time.Time) Decision {
	p.t.Helper()
	p.now = p.now.Add(after)
	l, d := Step(p.listen, Snapshot{TrackID: track, ProgressMS: progress, DurationMS: duration,
		IsPlaying: playing, PlayedAt: playedAt, At: p.now}, rules)
	p.listen = l
	if d.NewHistory {
		p.plays = append(p.plays, d.PlayedAt)
	}
	if d.Replay {
		p.replays++
	}
	return d
}

// play polls track every 3 s from progress `from` for `secs` seconds.
func (p *player) play(track string, fromMS int32, secs int, duration int32, playedAt time.Time) {
	p.t.Helper()
	for i := 0; i <= secs; i += 3 {
		p.snap(3*time.Second, track, ptr(fromMS+int32(i)*1000), duration, true, playedAt)
	}
}

func (p *player) want(plays, replays int) {
	p.t.Helper()
	if len(p.plays) != plays || p.replays != replays {
		p.t.Fatalf("plays=%d replays=%d, want %d and %d", len(p.plays), p.replays, plays, replays)
	}
}

const song = 200_000 // 200 s: 30% is 60 s

func TestSkipUnderThresholdDoesNotCount(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 50, song, t0)
	p.play("b", 0, 9, song, t0.Add(time.Minute))
	p.want(0, 0)
}

func TestCrossingThresholdCountsOnceWithStartTime(t *testing.T) {
	p := newPlayer(t)
	start := t0.Add(-time.Second)
	p.play("a", 0, 180, song, start)
	p.want(1, 0)
	if !p.plays[0].Equal(start) {
		t.Fatalf("played_at = %v, want listen start %v", p.plays[0], start)
	}
}

func TestPauseAndResumeAfterIdleDoesNotDoubleCount(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 90, song, t0) // counted at 60 s
	p.want(1, 0)
	// Paused, then idle (no snapshots) for 5 hours, then resumed where it was.
	p.snap(3*time.Second, "a", ptr(93_000), song, false, t0)
	p.now = p.now.Add(5 * time.Hour)
	p.play("a", 93_000, 90, song, t0.Add(5*time.Hour))
	p.want(1, 0)
}

func TestResumeCrossingThresholdCountsOnce(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 30, song, t0) // 30 s heard, not counted
	p.now = p.now.Add(2 * time.Hour)
	p.play("a", 33_000, 60, song, t0.Add(2*time.Hour))
	p.want(1, 0)
	if !p.plays[0].Equal(t0) {
		t.Fatalf("played_at = %v, want the first start %v", p.plays[0], t0)
	}
}

func TestResumeAfterWindowCountsAgain(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 90, song, t0)
	p.now = p.now.Add(25 * time.Hour)
	p.play("a", 93_000, 90, song, t0.Add(25*time.Hour))
	p.want(2, 0)
}

func TestSeekForwardDoesNotAddListeningTime(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 6, song, t0)
	p.snap(3*time.Second, "a", ptr(150_000), song, true, t0) // seek to 150 s
	p.play("a", 153_000, 30, song, t0)
	p.want(0, 0)
	if p.listen.ListenedMS > 60_000 {
		t.Fatalf("listened %d ms, the seek was counted", p.listen.ListenedMS)
	}
}

func TestSeekWhilePausedIsNotCounted(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 6, song, t0)
	p.snap(time.Minute, "a", ptr(120_000), song, false, t0) // paused, seeked
	p.snap(3*time.Second, "a", ptr(123_000), song, true, t0)
	if p.listen.ListenedMS > 15_000 {
		t.Fatalf("listened %d ms, the paused seek was counted", p.listen.ListenedMS)
	}
}

func TestSmallSeekBackIsNotANewPlay(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 90, song, t0)
	p.snap(3*time.Second, "a", ptr(85_000), song, true, t0) // 8 s back
	p.play("a", 85_000, 30, song, t0)
	p.want(1, 0)

	q := newPlayer(t) // not counted yet: even a bigger seek back mid-song keeps the listen
	q.play("a", 0, 30, song, t0)
	q.snap(3*time.Second, "a", ptr(15_000), song, true, t0)
	q.play("a", 15_000, 45, song, t0)
	q.want(1, 0)
}

func TestRestartFromZeroCountsReplayAtThreshold(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 120, song, t0)
	p.want(1, 0)
	p.play("a", 0, 30, song, t0) // back to 0: a new listen, not counted yet
	p.want(1, 0)
	p.play("a", 33_000, 40, song, t0)
	p.want(1, 1)
}

func TestRestartBeforeCountingIsStillOnePlay(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 40, song, t0)
	p.play("a", 0, 70, song, t0) // started over before 30%
	p.want(1, 0)
}

func TestRepeatOne(t *testing.T) {
	p := newPlayer(t)
	for i := 0; i < 3; i++ {
		p.play("a", 0, 198, song, t0)
	}
	p.want(1, 2)
}

func TestSwitchAwayAndBack(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 90, song, t0)
	p.play("b", 0, 90, song, t0.Add(2*time.Minute))
	p.play("a", 0, 90, song, t0.Add(4*time.Minute))
	p.want(3, 0)
}

func TestUnknownDurationNeeds30s(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 24, 0, t0)
	p.want(0, 0)
	p.play("a", 27_000, 9, 0, t0)
	p.want(1, 0)
}

func TestMissingProgress(t *testing.T) {
	p := newPlayer(t)
	for i := 0; i < 40; i++ {
		p.snap(3*time.Second, "a", nil, song, true, t0)
	}
	p.want(0, 0)
	p.play("a", 0, 66, song, t0) // progress known again: counts from here
	p.want(1, 0)
}

func TestSameSpotifyTimestampGetsANewStart(t *testing.T) {
	p := newPlayer(t)
	p.play("a", 0, 90, song, t0)
	p.play("b", 0, 90, song, t0) // Spotify reused the timestamp
	p.play("a", 0, 90, song, t0)
	p.want(3, 0)
	if p.plays[2].Equal(p.plays[0]) {
		t.Fatal("second listen of a reused played_at; the history row would be dropped")
	}
}

func TestDefaultsApplyToZeroRules(t *testing.T) {
	_, d := Step(Listen{}, Snapshot{TrackID: "a", ProgressMS: ptr(0), DurationMS: song, IsPlaying: true, At: t0}, PlayRules{})
	if d != (Decision{}) {
		t.Fatalf("first snapshot decided %+v", d)
	}
}
