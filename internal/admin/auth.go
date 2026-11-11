// Package admin implements the admin dashboard backend (/admin/api) and
// serves the embedded SPA (/admin).
package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/logging"
)

// User is the authenticated admin.
type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
	Role     string `json:"role,omitempty"`
}

type ctxKey struct{}

// TokenClaims are the custom session-token claims configured in Clerk
// (Configure → Sessions → Customize session token):
//
//	{"role": "{{user.public_metadata.role}}", "email": "{{user.primary_email_address}}"}
type TokenClaims struct {
	Role  string `json:"role"`
	Email string `json:"email"`
}

// UserFrom returns the admin user stored in the request context.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

// Authenticator verifies Clerk session tokens and enforces the allow-list.
type Authenticator struct {
	allowedEmails  []string
	allowedUserIDs []string
	adminRole      string
	parties        []string
	// allowLoopback accepts tokens minted for any http(s)://localhost,
	// 127.0.0.1 or [::1] origin (development only: dev servers move ports).
	allowLoopback bool
	rdb           *redis.Client
	log           *slog.Logger
	onForbidden   func(ctx context.Context, u User, ip string)
	// fetchUser reads a user from Clerk's Backend API (user.Get; tests
	// replace it).
	fetchUser func(ctx context.Context, id string) (*clerk.User, error)
	cache     profileCache // nil: Redis
}

// NewAuthenticator configures Clerk. authorizedParties are the origins
// allowed to mint tokens for this API (the dashboard origin).
func NewAuthenticator(secretKey string, allowedEmails, allowedUserIDs []string, adminRole string, authorizedParties []string, rdb *redis.Client, log *slog.Logger) *Authenticator {
	clerk.SetKey(secretKey)
	norm := make([]string, 0, len(allowedEmails))
	for _, e := range allowedEmails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			norm = append(norm, e)
		}
	}
	return &Authenticator{allowedEmails: norm, allowedUserIDs: allowedUserIDs, adminRole: adminRole,
		parties: authorizedParties, rdb: rdb, log: log}
}

// Middleware requires a valid Clerk session token for an allow-listed user.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	opts := []clerkhttp.AuthorizationOption{
		clerkhttp.AuthorizationFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			azp, iss := unverifiedClaims(r)
			a.log.WarnContext(r.Context(), "clerk token rejected (bad signature, expired, or unauthorized party)",
				slog.String("azp", azp), slog.String("iss", iss), slog.Any("allowed_parties", a.parties),
				slog.Bool("allow_loopback", a.allowLoopback))
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in required")
		})),
		clerkhttp.Leeway(10 * time.Second),
		clerkhttp.CustomClaimsConstructor(func(context.Context) any { return &TokenClaims{} }),
	}
	if len(a.parties) > 0 || a.allowLoopback {
		opts = append(opts, clerkhttp.AuthorizedParty(a.partyAllowed))
	}
	// WithHeaderAuthorization (not Require…) so missing/malformed tokens reach
	// our handler and get a JSON 401 instead of a bare 403.
	verify := clerkhttp.WithHeaderAuthorization(opts...)

	return verify(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := clerk.SessionClaimsFromContext(r.Context())
		if !ok || claims.Subject == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in required")
			return
		}
		tc, _ := claims.Custom.(*TokenClaims)
		a.authorize(w, r, next, claims.Subject, tc)
	}))
}

// authorize looks up the verified token's user and serves next if the user
// is an admin.
func (a *Authenticator) authorize(w http.ResponseWriter, r *http.Request, next http.Handler, subject string, tc *TokenClaims) {
	claimRole := ""
	if tc != nil {
		claimRole = tc.Role
	}
	u, err := a.lookup(r.Context(), subject, claimRole)
	switch {
	case err == nil:
		// Clerk answered: its profile decides. Token claims are used only
		// when Clerk cannot be reached (below).
	case errors.Is(err, errBlocked):
		if a.onForbidden != nil {
			a.onForbidden(r.Context(), u, clientIP(r))
		}
		writeError(w, http.StatusForbidden, "forbidden", "this account is banned or locked")
		return
	case errors.Is(err, errNoVerifiedEmail):
		if a.onForbidden != nil {
			a.onForbidden(r.Context(), u, clientIP(r))
		}
		writeError(w, http.StatusForbidden, "forbidden", "this account has no verified email")
		return
	case clerkUnavailable(err) && tc != nil && (tc.Role != "" || tc.Email != ""):
		// Clerk's Backend API is unreachable, but the verified token
		// carries enough to authorize; profile details stay empty.
		a.log.WarnContext(r.Context(), "clerk user lookup failed; authorizing from token claims", logging.Err(err))
		u = User{ID: subject, Email: tc.Email, Role: tc.Role}
	case clerkStatus(err) == http.StatusNotFound:
		// The token is valid but Clerk no longer has the user (deleted).
		a.log.WarnContext(r.Context(), "clerk user not found", logging.Err(err))
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in required")
		return
	default:
		a.log.ErrorContext(r.Context(), "clerk user lookup failed", logging.Err(err))
		// 503, not 502: the production proxy replaces 502 bodies with an
		// HTML error page, and the SPA needs this JSON.
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "could not verify the user with Clerk")
		return
	}
	if !a.allowed(u) {
		if a.onForbidden != nil {
			a.onForbidden(r.Context(), u, clientIP(r))
		}
		writeError(w, http.StatusForbidden, "forbidden", "this account is not an admin")
		return
	}
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
}

// errBlocked means Clerk has the user but the account is banned or locked.
var errBlocked = errors.New("user is banned or locked")

// errNoVerifiedEmail means Clerk has the user but no verified email address,
// and neither a user-ID allow-list nor a role can authorize them.
var errNoVerifiedEmail = errors.New("user has no verified email address")

// clerkStatus is the HTTP status of a Clerk API error, or 0 for any other
// error (network, timeout, decoding).
func clerkStatus(err error) int {
	var apiErr *clerk.APIErrorResponse
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatusCode
	}
	return 0
}

// clerkUnavailable reports whether err means Clerk could not answer (network
// error, 5xx or 429) rather than a definite answer about the user. Only then
// may the token claims stand in for the profile.
func clerkUnavailable(err error) bool {
	st := clerkStatus(err)
	return st == 0 || st >= 500 || st == http.StatusTooManyRequests
}

func (a *Authenticator) allowed(u User) bool {
	if slices.Contains(a.allowedUserIDs, u.ID) {
		return true
	}
	if a.adminRole != "" && u.Role == a.adminRole {
		return true
	}
	return u.Email != "" && slices.Contains(a.allowedEmails, strings.ToLower(u.Email))
}

// lookup fetches the user's profile from Clerk, cached in Redis for 5
// minutes. A cached profile whose role differs from the token's role claim
// (claimRole, "" when absent) is stale, e.g. the role was just granted or
// revoked: it is fetched again. The claim itself never sets the role.
func (a *Authenticator) lookup(ctx context.Context, id, claimRole string) (User, error) {
	key := "spotify-tracker:admin:user:" + id
	if raw, err := a.cacheGet(ctx, key); err == nil {
		var u User
		if json.Unmarshal(raw, &u) == nil && (claimRole == "" || u.Role == claimRole) {
			return u, nil
		}
	}
	fetch := a.fetchUser
	if fetch == nil {
		fetch = user.Get
	}
	cu, err := fetch(ctx, id)
	if err != nil {
		return User{}, err
	}
	if cu.Banned || cu.Locked {
		return User{ID: cu.ID}, errBlocked // never cached, never allowed
	}
	u := User{ID: cu.ID, Role: metadataRole(cu.PublicMetadata)}
	if cu.ImageURL != nil {
		u.ImageURL = *cu.ImageURL
	}
	var names []string
	if cu.FirstName != nil {
		names = append(names, *cu.FirstName)
	}
	if cu.LastName != nil {
		names = append(names, *cu.LastName)
	}
	u.Name = strings.Join(names, " ")
	for _, e := range cu.EmailAddresses {
		if e == nil {
			continue
		}
		verified := e.Verification != nil && e.Verification.Status == "verified"
		if !verified {
			continue
		}
		if u.Email == "" || (cu.PrimaryEmailAddressID != nil && e.ID == *cu.PrimaryEmailAddressID) {
			u.Email = e.EmailAddress
		}
	}
	if u.Email == "" && len(a.allowedUserIDs) == 0 && u.Role == "" {
		return u, errNoVerifiedEmail
	}
	if raw, err := json.Marshal(u); err == nil {
		a.cacheSet(ctx, key, raw)
	}
	return u, nil
}

func (a *Authenticator) cacheGet(ctx context.Context, key string) ([]byte, error) {
	if a.cache != nil {
		return a.cache.get(key)
	}
	return a.rdb.Get(ctx, key).Bytes()
}

func (a *Authenticator) cacheSet(ctx context.Context, key string, raw []byte) {
	if a.cache != nil {
		a.cache.set(key, raw)
		return
	}
	_ = a.rdb.Set(ctx, key, raw, 5*time.Minute).Err()
}

// profileCache replaces Redis for the profile cache in tests.
type profileCache interface {
	get(key string) ([]byte, error)
	set(key string, raw []byte)
}

// metadataRole reads "role" from Clerk public_metadata (set server-side only;
// users cannot edit public metadata).
func metadataRole(raw json.RawMessage) string {
	var m struct {
		Role string `json:"role"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return m.Role
}

// AllowLoopbackParties enables accepting tokens from any loopback origin.
func (a *Authenticator) AllowLoopbackParties() { a.allowLoopback = true }

// partyAllowed reports whether a token's azp (the origin that minted it) is
// accepted. Clerk omits azp for some token types; those are accepted.
func (a *Authenticator) partyAllowed(azp string) bool {
	if azp == "" || slices.Contains(a.parties, azp) {
		return true
	}
	if !a.allowLoopback {
		return false
	}
	u, err := url.Parse(azp)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// unverifiedClaims decodes azp/iss from the bearer token without verifying
// it — for diagnostics in logs only, never for decisions.
func unverifiedClaims(r *http.Request) (azp, iss string) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return "", ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	var c struct {
		Azp string `json:"azp"`
		Iss string `json:"iss"`
	}
	_ = json.Unmarshal(raw, &c)
	return c.Azp, c.Iss
}
