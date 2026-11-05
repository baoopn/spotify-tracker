// Package spotify is a minimal Spotify Web API client: OAuth (authorization
// code + refresh token grants, used by the API) and the "currently playing"
// endpoint (used by the poller with access tokens handed out by the API).
package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/spotify-tracker/internal/observability"
)

const (
	defaultAPIBase      = "https://api.spotify.com/v1"
	defaultAccountsBase = "https://accounts.spotify.com"
	// RequiredScope is needed for the currently-playing endpoint.
	RequiredScope = "user-read-currently-playing"
)

// Scopes requested when (re)authorizing.
var Scopes = []string{
	RequiredScope, "user-read-playback-state", "user-read-recently-played",
	// Playlist metadata for private / collaborative playlists you play from.
	"playlist-read-private", "playlist-read-collaborative",
}

// ErrInvalidGrant means the refresh token was revoked or expired (Spotify
// refresh tokens expire 180 days after authorization). Re-authorize.
var ErrInvalidGrant = errors.New("spotify: refresh token rejected (invalid_grant); re-authorize the app")

// ErrMissingScope means the token lacks user-read-currently-playing.
var ErrMissingScope = errors.New("spotify: token is missing scope " + RequiredScope)

// ErrNotConnected means no refresh token is configured.
var ErrNotConnected = errors.New("spotify: not connected; authorize it from the admin dashboard")

// RateLimitError is returned on HTTP 429.
type RateLimitError struct {
	RetryAfter time.Duration
	Reason     string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("spotify: rate limited (retry after %s, reason %q)", e.RetryAfter, e.Reason)
}

// Token is an OAuth token response.
type Token struct {
	AccessToken  string
	RefreshToken string // set when Spotify issues (or rotates) one
	ExpiresAt    time.Time
	Scopes       []string
}

// Profile is the Spotify user behind a token.
type Profile struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// OAuth performs Spotify OAuth flows for one app.
type OAuth struct {
	ClientID     string
	ClientSecret string
	AccountsBase string
	APIBase      string
	HTTP         *http.Client
}

func (o *OAuth) accounts() string {
	if o.AccountsBase != "" {
		return strings.TrimRight(o.AccountsBase, "/")
	}
	return defaultAccountsBase
}

func (o *OAuth) api() string {
	if o.APIBase != "" {
		return strings.TrimRight(o.APIBase, "/")
	}
	return defaultAPIBase
}

func (o *OAuth) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// AuthorizeURL builds the consent URL for the Authorization Code flow.
func (o *OAuth) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":     {o.ClientID},
		"response_type": {"code"},
		"redirect_uri":  {redirectURI},
		"scope":         {strings.Join(Scopes, " ")},
		"state":         {state},
		"show_dialog":   {"true"},
	}
	return o.accounts() + "/authorize?" + q.Encode()
}

// Exchange trades an authorization code for tokens.
func (o *OAuth) Exchange(ctx context.Context, code, redirectURI string) (Token, error) {
	return o.tokenRequest(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
}

// Refresh obtains a new access token from a refresh token.
func (o *OAuth) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	if refreshToken == "" {
		return Token{}, ErrNotConnected
	}
	return o.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (o *OAuth) tokenRequest(ctx context.Context, form url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.accounts()+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.SetBasicAuth(o.ClientID, o.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := o.client().Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("spotify: token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	observability.UpstreamRequests.WithLabelValues("spotify_accounts", strconv.Itoa(resp.StatusCode)).Inc()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tr struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &tr)
	if resp.StatusCode != http.StatusOK {
		if tr.Error == "invalid_grant" {
			return Token{}, ErrInvalidGrant
		}
		return Token{}, fmt.Errorf("spotify: token request failed (%d): %s %s", resp.StatusCode, tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return Token{}, errors.New("spotify: token response has no access_token")
	}
	t := Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		Scopes:       strings.Fields(tr.Scope),
	}
	if len(t.Scopes) > 0 && !HasScope(t.Scopes, RequiredScope) {
		return t, ErrMissingScope
	}
	return t, nil
}

// Me returns the profile of the token's user.
func (o *OAuth) Me(ctx context.Context, accessToken string) (Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.api()+"/me", nil)
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := o.client().Do(req)
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("spotify: /me failed (%d)", resp.StatusCode)
	}
	var p Profile
	return p, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p)
}

// HasScope reports whether want is in scopes.
func HasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

func truncate(b []byte) string {
	const max = 300
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
