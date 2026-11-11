package spotify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
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
	// load and save override the settings store (tests only).
	load func(ctx context.Context) (settings.Spotify, error)
	save func(ctx context.Context, actor string, s settings.Spotify) error

	// alerting is set while an OnInvalidGrant call is in flight.
	alerting atomic.Bool

	mu sync.Mutex
	// unsaved is a rotated refresh token that could not be saved; it is
	// used over the stored one until a save succeeds. Guarded by mu.
	unsaved string
}

const (
	// alertTimeout bounds one OnInvalidGrant call.
	alertTimeout = 60 * time.Second
	// saveTimeout bounds saveState, all attempts included.
	saveTimeout  = 10 * time.Second
	saveAttempts = 3
	// errorSaveTimeout bounds the single save of last_error after a failed
	// refresh that did not rotate the token.
	errorSaveTimeout = 2 * time.Second
)

// saveBackoff is the wait before each retry in saveState.
var saveBackoff = []time.Duration{250 * time.Millisecond, time.Second}

// ErrTokenNotSaved means Spotify rotated the refresh token but the new one
// could not be saved. The Manager keeps it in memory and retries the save on
// the next refresh.
var ErrTokenNotSaved = errors.New("spotify: rotated refresh token could not be saved")

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
	tok, exp, err := m.refreshLocked(ctx, force)
	if errors.Is(err, ErrInvalidGrant) {
		m.alertInvalidGrant(ctx, err)
	}
	return tok, exp, err
}

// refreshLocked refreshes the access token under m.mu. It never calls
// OnInvalidGrant, so a slow alert cannot hold the lock.
func (m *Manager) refreshLocked(ctx context.Context, force bool) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Another caller may have refreshed while we waited for the lock.
	if !force {
		if tok, exp, ok := m.cached(ctx); ok {
			return tok, exp, nil
		}
	}

	cur, err := m.loadState(ctx)
	if errors.Is(err, settings.ErrNotFound) || (err == nil && cur.RefreshToken == "" && m.unsaved == "") {
		return "", time.Time{}, ErrNotConnected
	}
	if err != nil {
		return "", time.Time{}, err
	}

	stored := cur.RefreshToken
	refresh := stored
	if m.unsaved != "" {
		// A rotated token that could not be saved is the live one.
		refresh = m.unsaved
	}
	tok, err := m.oauth.Refresh(ctx, refresh)
	now := time.Now().UTC()
	if err != nil {
		cur.LastError = err.Error()
		// Some failures (ErrMissingScope) still carry a rotated refresh
		// token; the old one may already be revoked, so keep the new one.
		cur.RefreshToken = refresh
		if tok.RefreshToken != "" {
			cur.RefreshToken = tok.RefreshToken
		}
		switch {
		case cur.RefreshToken != stored:
			// A rotated token must survive: save it with retries.
			if saveErr := m.saveState(ctx, "system", cur); saveErr != nil {
				m.unsaved = cur.RefreshToken
				m.log.ErrorContext(ctx, "save rotated spotify refresh token failed", slog.Any("error", saveErr))
			} else {
				m.unsaved = ""
			}
		case ctx.Err() != nil:
			// The caller went away; that is not a Spotify error worth
			// recording in last_error.
		default:
			// Only last_error changes: one short attempt, so a database
			// outage does not hold the lock on every failed refresh.
			if saveErr := m.saveOnce(ctx, "system", cur); saveErr != nil {
				m.log.WarnContext(ctx, "save spotify error state failed", slog.Any("error", saveErr))
			}
		}
		return "", time.Time{}, err
	}

	cur.LastRefreshAt = &now
	cur.LastError = ""
	if len(tok.Scopes) > 0 {
		cur.Scopes = tok.Scopes
	}
	cur.RefreshToken = refresh
	if tok.RefreshToken != "" {
		// Spotify may rotate the refresh token; keep the newest.
		cur.RefreshToken = tok.RefreshToken
	}
	rotated := cur.RefreshToken != stored
	if err := m.saveState(ctx, "system", cur); err != nil {
		if rotated {
			// The old token may already be revoked: keep the new one in
			// memory for the next refresh and do not hand out an access
			// token until it is saved.
			m.unsaved = cur.RefreshToken
			m.log.ErrorContext(ctx, "save rotated spotify refresh token failed", slog.Any("error", err))
			return "", time.Time{}, fmt.Errorf("%w: %w", ErrTokenNotSaved, err)
		}
		m.log.WarnContext(ctx, "save spotify token state failed", slog.Any("error", err))
	} else {
		m.unsaved = ""
	}
	m.cache(ctx, tok.AccessToken, tok.ExpiresAt)
	return tok.AccessToken, tok.ExpiresAt, nil
}

// alertInvalidGrant calls OnInvalidGrant in the background with a context
// that outlives the caller's, at most one call at a time.
func (m *Manager) alertInvalidGrant(ctx context.Context, err error) {
	if m.OnInvalidGrant == nil || !m.alerting.CompareAndSwap(false, true) {
		return
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alertTimeout)
	go func() {
		defer m.alerting.Store(false)
		defer cancel()
		m.OnInvalidGrant(actx, err)
	}()
}

// saveOnce makes a single save attempt with its own short deadline,
// detached from the caller.
func (m *Manager) saveOnce(ctx context.Context, actor string, s settings.Spotify) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), errorSaveTimeout)
	defer cancel()
	return m.saver()(ctx, actor, s)
}

func (m *Manager) saver() func(ctx context.Context, actor string, s settings.Spotify) error {
	if m.save != nil {
		return m.save
	}
	return m.settings.SaveSpotify
}

func (m *Manager) loadState(ctx context.Context) (settings.Spotify, error) {
	if m.load != nil {
		return m.load(ctx)
	}
	return m.settings.Spotify(ctx)
}

// saveState stores s, retrying briefly. It uses its own deadline, detached
// from the caller, so a cancelled request cannot drop a rotated token.
func (m *Manager) saveState(ctx context.Context, actor string, s settings.Spotify) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
	defer cancel()
	save := m.saver()
	var err error
	for i := range saveAttempts {
		if err = save(ctx, actor, s); err == nil {
			return nil
		}
		if i == len(saveBackoff) {
			break
		}
		t := time.NewTimer(saveBackoff[i])
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
	}
	return err
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
	if err := m.saveState(ctx, actor, s); err != nil {
		return s, err
	}
	m.unsaved = ""
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
