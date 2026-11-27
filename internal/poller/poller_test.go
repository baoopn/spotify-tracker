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

func TestMidpoint(t *testing.T) {
	a := time.Date(2026, 11, 27, 10, 0, 0, 0, time.FixedZone("x", 3600))
	if got := midpoint(a, a.Add(500*time.Millisecond)); !got.Equal(a.Add(250*time.Millisecond)) || got.Location() != time.UTC {
		t.Errorf("midpoint = %v", got)
	}
}
