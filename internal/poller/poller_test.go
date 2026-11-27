package poller

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// slowSource answers after delay, as a slow Spotify request.
type slowSource struct {
	delay time.Duration
	pb    *domain.Playback
}

func (s *slowSource) CurrentlyPlaying(context.Context) (*domain.Playback, error) {
	time.Sleep(s.delay)
	if s.pb == nil {
		return nil, nil
	}
	cp := *s.pb
	cp.FetchedAt = time.Now() // the client stamps the response time
	return &cp, nil
}

// stampIngest records the fetched_at of each request.
type stampIngest struct {
	fakeIngest
	mu      sync.Mutex
	records []time.Time
	clears  []time.Time
}

func (s *stampIngest) RecordPlayback(_ context.Context, r *trackerv1.RecordPlaybackRequest, _ ...grpc.CallOption) (*trackerv1.RecordPlaybackResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r.GetPlayback().GetFetchedAt().AsTime())
	return &trackerv1.RecordPlaybackResponse{}, nil
}

func (s *stampIngest) ClearPlayback(_ context.Context, r *trackerv1.ClearPlaybackRequest, _ ...grpc.CallOption) (*trackerv1.ClearPlaybackResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clears = append(s.clears, r.GetFetchedAt().AsTime())
	return &trackerv1.ClearPlaybackResponse{}, nil
}

// inMiddle checks that got is about halfway between before and after.
func inMiddle(t *testing.T, name string, before, got, after time.Time) {
	t.Helper()
	mid := before.Add(after.Sub(before) / 2)
	if d := got.Sub(mid); d < -40*time.Millisecond || d > 40*time.Millisecond {
		t.Errorf("%s fetched_at %v is %v from the midpoint (request %v .. %v)", name, got, d, before, after)
	}
}

func TestFetchedAtIsRequestMidpoint(t *testing.T) {
	const delay = 300 * time.Millisecond
	src := &slowSource{delay: delay, pb: &domain.Playback{Track: domain.Track{ID: "t1", Name: "Song"}, IsPlaying: true}}
	ing := &stampIngest{}
	p := New(src, ing, time.Second, nopHeartbeat{}, slog.New(slog.DiscardHandler))

	before := time.Now()
	if _, err := p.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	if len(ing.records) != 1 {
		t.Fatalf("records = %v", ing.records)
	}
	inMiddle(t, "record", before, ing.records[0], after)
	if ing.records[0].Location() != time.UTC {
		t.Errorf("not UTC: %v", ing.records[0])
	}

	// Nothing playing now: ClearPlayback gets the midpoint too.
	src.pb = nil
	before = time.Now()
	if _, err := p.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	after = time.Now()
	if len(ing.clears) != 1 {
		t.Fatalf("clears = %v", ing.clears)
	}
	inMiddle(t, "clear", before, ing.clears[0], after)
}

// The poller smooths progress with the anchor, passes changed_at through and
// keeps played_at == fetched_at when Spotify sent no timestamp.
func TestCycleAnchorsProgress(t *testing.T) {
	changed := time.Now().Add(-10 * time.Second).UTC()
	var sent []*trackerv1.Playback
	ing := &captureIngest{onRecord: func(pb *trackerv1.Playback) { sent = append(sent, pb) }}
	src := &scriptedSource{}
	p := New(src, ing, time.Second, nopHeartbeat{}, slog.New(slog.DiscardHandler))
	progress := func(off int32) *domain.Playback {
		// Raw progress: 10 s into the state plus off ms of response jitter.
		v := int32(time.Since(changed).Milliseconds()) + off
		return &domain.Playback{Track: domain.Track{ID: "t1", DurationMS: 300000}, IsPlaying: true,
			ProgressMS: &v, ChangedAt: &changed, PlayedAt: changed}
	}
	for _, off := range []int32{0, 120, -90, 10, 140} {
		src.next = progress(off)
		if _, err := p.cycle(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	last := sent[len(sent)-1]
	if last.GetChangedAt() == nil || !last.GetChangedAt().AsTime().Equal(changed) {
		t.Errorf("changed_at = %v", last.GetChangedAt())
	}
	// The published value follows the median base (jitter 10 ms), not the
	// last raw report (+140 ms).
	elapsed := last.GetFetchedAt().AsTime().Sub(changed).Milliseconds()
	if d := int64(last.GetProgressMs()) - elapsed; d < -30 || d > 50 {
		t.Errorf("published progress is %d ms off the anchor", d)
	}

	// No Spotify timestamp: raw progress, and played_at equals fetched_at.
	v := int32(5000)
	src.next = &domain.Playback{Track: domain.Track{ID: "t2"}, IsPlaying: true, ProgressMS: &v}
	if _, err := p.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	last = sent[len(sent)-1]
	if last.GetProgressMs() != 5000 || last.GetChangedAt() != nil || !last.GetPlayedAt().AsTime().Equal(last.GetFetchedAt().AsTime()) {
		t.Errorf("no timestamp = %+v", last)
	}
}

type scriptedSource struct{ next *domain.Playback }

func (s *scriptedSource) CurrentlyPlaying(context.Context) (*domain.Playback, error) {
	if s.next == nil {
		return nil, nil
	}
	cp := *s.next
	return &cp, nil
}

type captureIngest struct {
	fakeIngest
	onRecord func(*trackerv1.Playback)
}

func (c *captureIngest) RecordPlayback(_ context.Context, r *trackerv1.RecordPlaybackRequest, _ ...grpc.CallOption) (*trackerv1.RecordPlaybackResponse, error) {
	c.onRecord(r.GetPlayback())
	return &trackerv1.RecordPlaybackResponse{}, nil
}

func TestMidpoint(t *testing.T) {
	a := time.Date(2026, 11, 27, 10, 0, 0, 0, time.FixedZone("x", 3600))
	if got := midpoint(a, a.Add(500*time.Millisecond)); !got.Equal(a.Add(250*time.Millisecond)) || got.Location() != time.UTC {
		t.Errorf("midpoint = %v", got)
	}
}

// timedFake reports its own request time, as *spotify.Player does.
type timedFake struct {
	pb *domain.Playback
	rt spotify.RequestTime
}

func (f *timedFake) CurrentlyPlaying(context.Context) (*domain.Playback, error) { return f.pb, nil }
func (f *timedFake) CurrentlyPlayingTimed(context.Context) (*domain.Playback, spotify.RequestTime, error) {
	time.Sleep(200 * time.Millisecond) // a slow token refresh before the request
	return f.pb, f.rt, nil
}

// The poller uses the source's request timing, not the time around the
// whole call (which includes token lookups and retries).
func TestFetchedAtUsesSourceRequestTime(t *testing.T) {
	sent := time.Now().Add(-time.Second)
	rt := spotify.RequestTime{Sent: sent, Received: sent.Add(100 * time.Millisecond)}
	src := &timedFake{pb: &domain.Playback{Track: domain.Track{ID: "t1"}, IsPlaying: true}, rt: rt}
	ing := &stampIngest{}
	p := New(src, ing, time.Second, nopHeartbeat{}, slog.New(slog.DiscardHandler))
	if _, err := p.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ing.records) != 1 || !ing.records[0].Equal(rt.Mid()) {
		t.Errorf("record fetched_at = %v, want %v", ing.records, rt.Mid())
	}
	src.pb = nil
	if _, err := p.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ing.clears) != 1 || !ing.clears[0].Equal(rt.Mid()) {
		t.Errorf("clear fetched_at = %v, want %v", ing.clears, rt.Mid())
	}
}
