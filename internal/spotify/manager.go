package spotify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

// Manager owns the Spotify refresh token (stored encrypted in settings) and
// hands out cached access tokens. It runs inside the API; workers obtain
// access tokens from it over gRPC and never see the client secret or the
// refresh token.
type Manager struct {
	oauth    *OAuth
	settings *settings.Store
	rdb      *redis.Client
	log      *slog.Logger
	// OnInvalidGrant is called (best effort) when Spotify rejects the token.
	OnInvalidGrant func(ctx context.Context, err error)
	// OwnerID, when set, restricts Connect to this Spotify user ID.
	OwnerID string

	// tokens overrides AccessToken (tests only).
	tokens func(ctx context.Context, force bool) (string, time.Time, error)

	mu sync.Mutex
}

// NewManager creates a Manager.
func NewManager(o *OAuth, st *settings.Store, rdb *redis.Client, log *slog.Logger) *Manager {
	return &Manager{oauth: o, settings: st, rdb: rdb, log: log}
}

// OAuth exposes the underlying OAuth client.
func (m *Manager) OAuth() *OAuth { return m.oauth }

// AccessToken returns a valid access token, refreshing when it expires within
// a minute or when force is set.
func (m *Manager) AccessToken(ctx context.Context, force bool) (string, time.Time, error) {
	if !force {
		if tok, exp, ok := m.cached(ctx); ok {
			return tok, exp, nil
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Another caller may have refreshed while we waited for the lock.
	if !force {
		if tok, exp, ok := m.cached(ctx); ok {
			return tok, exp, nil
		}
	}

	cur, err := m.settings.Spotify(ctx)
	if errors.Is(err, settings.ErrNotFound) || (err == nil && cur.RefreshToken == "") {
		return "", time.Time{}, ErrNotConnected
	}
	if err != nil {
		return "", time.Time{}, err
	}

	tok, err := m.oauth.Refresh(ctx, cur.RefreshToken)
	now := time.Now().UTC()
	if err != nil {
		cur.LastError = err.Error()
		if saveErr := m.settings.SaveSpotify(ctx, "system", cur); saveErr != nil {
			m.log.WarnContext(ctx, "save spotify error state failed", slog.Any("error", saveErr))
		}
		if errors.Is(err, ErrInvalidGrant) && m.OnInvalidGrant != nil {
			m.OnInvalidGrant(ctx, err)
		}
		return "", time.Time{}, err
	}

	cur.LastRefreshAt = &now
	cur.LastError = ""
	if len(tok.Scopes) > 0 {
		cur.Scopes = tok.Scopes
	}
	if tok.RefreshToken != "" && tok.RefreshToken != cur.RefreshToken {
		// Spotify rotated the refresh token; keep the newest.
		cur.RefreshToken = tok.RefreshToken
	}
	if err := m.settings.SaveSpotify(ctx, "system", cur); err != nil {
		m.log.WarnContext(ctx, "save spotify token state failed", slog.Any("error", err))
	}
	m.cache(ctx, tok.AccessToken, tok.ExpiresAt)
	return tok.AccessToken, tok.ExpiresAt, nil
}

// token returns an access token for Web API calls made by the Manager.
func (m *Manager) token(ctx context.Context, force bool) (string, error) {
	var tok string
	var err error
	if m.tokens != nil {
		tok, _, err = m.tokens(ctx, force)
	} else {
		tok, _, err = m.AccessToken(ctx, force)
	}
	return tok, err
}

// Connect stores the result of a completed Authorization Code flow.
func (m *Manager) Connect(ctx context.Context, actor, code, redirectURI string) (settings.Spotify, error) {
	tok, err := m.oauth.Exchange(ctx, code, redirectURI)
	if err != nil {
		return settings.Spotify{}, err
	}
	if tok.RefreshToken == "" {
		return settings.Spotify{}, errors.New("spotify: authorization returned no refresh token")
	}
	now := time.Now().UTC()
	s := settings.Spotify{
		RefreshToken:  tok.RefreshToken,
		AuthorizedAt:  &now,
		Scopes:        tok.Scopes,
		Source:        "dashboard",
		LastRefreshAt: &now,
	}
	p, err := m.oauth.Me(ctx, tok.AccessToken)
	switch {
	case err == nil:
		s.AccountID, s.AccountName = p.ID, p.DisplayName
	case m.OwnerID != "":
		return settings.Spotify{}, fmt.Errorf("spotify: could not verify the account owner: %w", err)
	default:
		m.log.WarnContext(ctx, "spotify profile lookup failed", slog.Any("error", err))
	}
	if m.OwnerID != "" && s.AccountID != m.OwnerID {
		return settings.Spotify{}, fmt.Errorf("spotify: account %q is not the configured owner; sign in to Spotify with the right account", s.AccountName)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.settings.SaveSpotify(ctx, actor, s); err != nil {
		return s, err
	}
	m.cache(ctx, tok.AccessToken, tok.ExpiresAt)
	return s, nil
}

func (m *Manager) cached(ctx context.Context) (string, time.Time, bool) {
	vals, err := m.rdb.HMGet(ctx, redisx.KeySpotifyAccessToken, "token", "expires_at").Result()
	if err != nil || len(vals) != 2 {
		return "", time.Time{}, false
	}
	tok, _ := vals[0].(string)
	expStr, _ := vals[1].(string)
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if tok == "" || err != nil {
		return "", time.Time{}, false
	}
	expiresAt := time.Unix(exp, 0)
	if time.Until(expiresAt) <= time.Minute {
		return "", time.Time{}, false
	}
	return tok, expiresAt, true
}

func (m *Manager) cache(ctx context.Context, token string, expiresAt time.Time) {
	pipe := m.rdb.TxPipeline()
	pipe.HSet(ctx, redisx.KeySpotifyAccessToken, "token", token, "expires_at", strconv.FormatInt(expiresAt.Unix(), 10))
	pipe.ExpireAt(ctx, redisx.KeySpotifyAccessToken, expiresAt)
	if _, err := pipe.Exec(ctx); err != nil {
		m.log.WarnContext(ctx, "cache spotify access token failed", slog.Any("error", err))
	}
}

// Invalidate drops the cached access token (e.g. after re-authorizing).
func (m *Manager) Invalidate(ctx context.Context) error {
	if err := m.rdb.Del(ctx, redisx.KeySpotifyAccessToken).Err(); err != nil {
		return fmt.Errorf("invalidate spotify token: %w", err)
	}
	return nil
}
