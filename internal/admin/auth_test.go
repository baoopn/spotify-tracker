package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/redis/go-redis/v9"
)

// newTestAuth builds an Authenticator whose Clerk lookups go to fetch and
// whose Redis is unreachable (so the profile cache always misses).
func newTestAuth(t *testing.T, fetch func(ctx context.Context, id string) (*clerk.User, error)) (*Authenticator, *[]User) {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	var forbidden []User
	a := &Authenticator{
		allowedEmails: []string{"admin@example.com"},
		adminRole:     "admin",
		rdb:           rdb,
		log:           slog.New(slog.DiscardHandler),
		fetchUser:     fetch,
		onForbidden:   func(_ context.Context, u User, _ string) { forbidden = append(forbidden, u) },
	}
	return a, &forbidden
}

// serve runs authorize for subject with claims tc and returns the response
// and whether next was reached.
func serve(a *Authenticator, subject string, tc *TokenClaims) (*httptest.ResponseRecorder, bool) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	a.authorize(rec, httptest.NewRequest(http.MethodGet, "/admin/api/me", nil), next, subject, tc)
	return rec, reached
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) (code, msg string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad error body %q: %v", rec.Body.String(), err)
	}
	return body.Error.Code, body.Error.Message
}

func TestAdminNoVerifiedEmail403(t *testing.T) {
	a, forbidden := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		return &clerk.User{
			ID:             id,
			EmailAddresses: []*clerk.EmailAddress{{ID: "e1", EmailAddress: "admin@example.com", Verification: &clerk.Verification{Status: "unverified"}}},
		}, nil
	})
	// Even claims that would pass must not override a definite Clerk answer.
	rec, reached := serve(a, "user_1", &TokenClaims{Email: "admin@example.com"})
	if reached {
		t.Fatal("user without a verified email reached the handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if code, msg := errorCode(t, rec); code != "forbidden" || msg != "this account has no verified email" {
		t.Errorf("error = %q %q", code, msg)
	}
	if len(*forbidden) != 1 || (*forbidden)[0].ID != "user_1" {
		t.Errorf("onForbidden calls = %+v, want one for user_1", *forbidden)
	}
}

func TestAdminVerifiedEmailAllowed(t *testing.T) {
	a, _ := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		return &clerk.User{ID: id, EmailAddresses: []*clerk.EmailAddress{{ID: "e1", EmailAddress: "Admin@example.com", Verification: &clerk.Verification{Status: "verified"}}}}, nil
	})
	if rec, reached := serve(a, "user_1", nil); !reached {
		t.Fatalf("verified admin refused: %d %s", rec.Code, rec.Body)
	}
}

func TestAdminClaimsFallbackOnlyOnOutage(t *testing.T) {
	apiErr := func(status int) error {
		return &clerk.APIErrorResponse{HTTPStatusCode: status}
	}
	claims := &TokenClaims{Role: "admin", Email: "admin@example.com"}
	for _, c := range []struct {
		name     string
		err      error
		status   int
		fallback bool
	}{
		{"network", errors.New("dial tcp: connection refused"), http.StatusNoContent, true},
		{"503", apiErr(http.StatusServiceUnavailable), http.StatusNoContent, true},
		{"429", apiErr(http.StatusTooManyRequests), http.StatusNoContent, true},
		{"404", apiErr(http.StatusNotFound), http.StatusUnauthorized, false},
		{"400", apiErr(http.StatusBadRequest), http.StatusServiceUnavailable, false},
		{"403", apiErr(http.StatusForbidden), http.StatusServiceUnavailable, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _ := newTestAuth(t, func(context.Context, string) (*clerk.User, error) { return nil, c.err })
			rec, reached := serve(a, "user_1", claims)
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.status, rec.Body)
			}
			if reached != c.fallback {
				t.Errorf("reached handler = %v, want %v", reached, c.fallback)
			}
		})
	}

	// Without usable claims an outage is a JSON 503 auth_unavailable.
	a, _ := newTestAuth(t, func(context.Context, string) (*clerk.User, error) { return nil, errors.New("timeout") })
	rec, _ := serve(a, "user_1", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("outage without claims: status = %d, want 503", rec.Code)
	}
	if code, _ := errorCode(t, rec); code != "auth_unavailable" {
		t.Errorf("outage without claims: code = %q, want auth_unavailable", code)
	}
}

func TestClerkUnavailable(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{errors.New("eof"), true},
		{context.DeadlineExceeded, true},
		{&clerk.APIErrorResponse{HTTPStatusCode: 500}, true},
		{&clerk.APIErrorResponse{HTTPStatusCode: 429}, true},
		{&clerk.APIErrorResponse{HTTPStatusCode: 404}, false},
		{&clerk.APIErrorResponse{HTTPStatusCode: 401}, false},
	} {
		if got := clerkUnavailable(c.err); got != c.want {
			t.Errorf("clerkUnavailable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
