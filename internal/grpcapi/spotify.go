package grpcapi

import (
	"context"
	"errors"
	"sync"
	"time"

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
type TokenSource struct {
	client trackerv1.SpotifyServiceClient

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewTokenSource creates a TokenSource.
func NewTokenSource(c trackerv1.SpotifyServiceClient) *TokenSource { return &TokenSource{client: c} }

// AccessToken returns a cached token or fetches a new one from the API.
func (t *TokenSource) AccessToken(ctx context.Context, force bool) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !force && t.token != "" && time.Until(t.expires) > time.Minute {
		return t.token, nil
	}
	resp, err := t.client.GetAccessToken(ctx, &trackerv1.GetAccessTokenRequest{ForceRefresh: force})
	if err != nil {
		t.token = ""
		if status.Code(err) == codes.FailedPrecondition {
			return "", errors.Join(ErrSpotifyAuth, errors.New(status.Convert(err).Message()))
		}
		return "", err
	}
	t.token, t.expires = resp.GetAccessToken(), resp.GetExpiresAt().AsTime()
	return t.token, nil
}
