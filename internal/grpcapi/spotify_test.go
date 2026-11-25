package grpcapi

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
)

type slowTokens struct {
	calls   atomic.Int32
	release chan struct{} // when set, a forced call waits for it
	err     error
}

func (s *slowTokens) GetAccessToken(ctx context.Context, r *trackerv1.GetAccessTokenRequest, _ ...grpc.CallOption) (*trackerv1.GetAccessTokenResponse, error) {
	n := s.calls.Add(1)
	if r.GetForceRefresh() && s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &trackerv1.GetAccessTokenResponse{
		AccessToken: "tok" + strconv.Itoa(int(n)), ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
	}, nil
}

// A forced refresh in flight never blocks a caller that can use the cached
// token (the 3 s poll keeps running while the queue read refreshes).
func TestTokenSourceRefreshDoesNotBlock(t *testing.T) {
	c := &slowTokens{release: make(chan struct{})}
	ts := NewTokenSource(c)
	if tok, err := ts.AccessToken(context.Background(), false); err != nil || tok != "tok1" {
		t.Fatalf("first = %q, %v", tok, err)
	}
	done := make(chan string)
	go func() {
		tok, _ := ts.AccessToken(context.Background(), true)
		done <- tok
	}()
	for c.calls.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	got := make(chan string, 1)
	go func() {
		tok, _ := ts.AccessToken(context.Background(), false)
		got <- tok
	}()
	select {
	case tok := <-got:
		if tok != "tok1" {
			t.Errorf("cached token = %q", tok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cached read waited for the refresh")
	}
	close(c.release)
	if tok := <-done; tok != "tok2" {
		t.Errorf("forced = %q", tok)
	}
}

// Concurrent fetches share one call.
func TestTokenSourceSharesFetch(t *testing.T) {
	c := &slowTokens{release: make(chan struct{})}
	ts := NewTokenSource(c)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ts.AccessToken(context.Background(), true)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(c.release)
	wg.Wait()
	if n := c.calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestTokenSourceAuthError(t *testing.T) {
	ts := NewTokenSource(&slowTokens{err: status.Error(codes.FailedPrecondition, "re-authorize")})
	if _, err := ts.AccessToken(context.Background(), false); err == nil || !errors.Is(err, ErrSpotifyAuth) {
		t.Fatalf("err = %v", err)
	}
}

// scriptTokens answers each call with the next scripted reply.
type scriptTokens struct {
	mu      sync.Mutex
	replies []tokenReply
	calls   int
}

type tokenReply struct {
	tok string
	exp time.Time
	err error
}

func (s *scriptTokens) GetAccessToken(context.Context, *trackerv1.GetAccessTokenRequest, ...grpc.CallOption) (*trackerv1.GetAccessTokenResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.replies[s.calls]
	s.calls++
	if r.err != nil {
		return nil, r.err
	}
	return &trackerv1.GetAccessTokenResponse{AccessToken: r.tok, ExpiresAt: timestamppb.New(r.exp)}, nil
}

// A failed forced refresh drops the cached token (Spotify rejected it), so
// the next call fetches again instead of reusing it.
func TestTokenSourceForcedFailureClears(t *testing.T) {
	hour := time.Now().Add(time.Hour)
	c := &scriptTokens{replies: []tokenReply{
		{tok: "old", exp: hour},
		{err: status.Error(codes.Unavailable, "down")},
		{tok: "new", exp: hour.Add(time.Minute)},
	}}
	ts := NewTokenSource(c)
	if tok, err := ts.AccessToken(context.Background(), false); err != nil || tok != "old" {
		t.Fatalf("first = %q, %v", tok, err)
	}
	if _, err := ts.AccessToken(context.Background(), true); err == nil {
		t.Fatal("forced refresh did not fail")
	}
	if tok, err := ts.AccessToken(context.Background(), false); err != nil || tok != "new" {
		t.Errorf("after the failed refresh = %q, %v (want a new fetch)", tok, err)
	}
}

// A fetch that returns a token expiring sooner than the cached one does not
// replace it.
func TestTokenSourceKeepsNewerToken(t *testing.T) {
	later := time.Now().Add(2 * time.Hour)
	c := &scriptTokens{replies: []tokenReply{
		{tok: "newer", exp: later},
		{tok: "older", exp: later.Add(-time.Hour)},
	}}
	ts := NewTokenSource(c)
	if _, err := ts.fetch(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.fetch(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if tok, ok := ts.cached(); !ok || tok != "newer" {
		t.Errorf("cached = %q, %v", tok, ok)
	}
}
