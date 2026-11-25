package grpcapi

import (
	"context"
	"errors"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// ─── Server ───────────────────────────────────────────────────────────────────

type spotifyServer struct {
	trackerv1.UnimplementedSpotifyServiceServer
	mgr *spotify.Manager
}

func (s *spotifyServer) GetAccessToken(ctx context.Context, req *trackerv1.GetAccessTokenRequest) (*trackerv1.GetAccessTokenResponse, error) {
	tok, exp, err := s.mgr.AccessToken(ctx, req.GetForceRefresh())
	switch {
	case errors.Is(err, spotify.ErrNotConnected), errors.Is(err, spotify.ErrInvalidGrant), errors.Is(err, spotify.ErrMissingScope):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case err != nil:
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return &trackerv1.GetAccessTokenResponse{AccessToken: tok, ExpiresAt: timestamppb.New(exp)}, nil
}

// ─── Client-side token source (poller) ────────────────────────────────────────

// ErrSpotifyAuth wraps FailedPrecondition answers: the operator must
// (re)authorize Spotify in the admin dashboard.
var ErrSpotifyAuth = errors.New("spotify authorization required")

// TokenSource implements spotify.TokenSource over gRPC with a local cache.
// The cache lock is never held across the gRPC call: concurrent refreshes
// share one call (singleflight), and a caller that does not force a refresh
// keeps using the cached token while another one refreshes.
type TokenSource struct {
	client trackerv1.SpotifyServiceClient
	flight singleflight.Group

	mu      sync.Mutex
	token   string
	expires time.Time
}

// tokenFetchTimeout bounds one shared token fetch.
const tokenFetchTimeout = 10 * time.Second

// NewTokenSource creates a TokenSource.
func NewTokenSource(c trackerv1.SpotifyServiceClient) *TokenSource { return &TokenSource{client: c} }

// AccessToken returns a cached token or fetches a new one from the API.
func (t *TokenSource) AccessToken(ctx context.Context, force bool) (string, error) {
	if !force {
		if tok, ok := t.cached(); ok {
			return tok, nil
		}
	}
	key := "fetch"
	if force {
		key = "force"
	}
	ch := t.flight.DoChan(key, func() (any, error) {
		// Detached from the first caller, so its cancellation does not fail
		// everyone waiting on the same fetch.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenFetchTimeout)
		defer cancel()
		return t.fetch(fctx, force)
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return "", r.Err
		}
		return r.Val.(string), nil
	}
}

func (t *TokenSource) cached() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.token, t.token != "" && time.Until(t.expires) > time.Minute
}

func (t *TokenSource) fetch(ctx context.Context, force bool) (string, error) {
	resp, err := t.client.GetAccessToken(ctx, &trackerv1.GetAccessTokenRequest{ForceRefresh: force})
	if err != nil {
		if status.Code(err) == codes.FailedPrecondition {
			t.mu.Lock()
			t.token = ""
			t.mu.Unlock()
			return "", errors.Join(ErrSpotifyAuth, errors.New(status.Convert(err).Message()))
		}
		return "", err
	}
	t.mu.Lock()
	t.token, t.expires = resp.GetAccessToken(), resp.GetExpiresAt().AsTime()
	t.mu.Unlock()
	return resp.GetAccessToken(), nil
}
