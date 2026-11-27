package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"

	"github.com/baoopn/spotify-tracker/internal/store"
)

func verifiedUser(id, email, role string) *clerk.User {
	meta := json.RawMessage(`{}`)
	if role != "" {
		meta = json.RawMessage(`{"role":"` + role + `"}`)
	}
	return &clerk.User{ID: id, PublicMetadata: meta,
		EmailAddresses: []*clerk.EmailAddress{{ID: "e1", EmailAddress: email, Verification: &clerk.Verification{Status: "verified"}}}}
}

// serveListener runs the listener checks (after token verification) for
// subject and returns the response and the listener the handler saw.
func serveListener(a *Authenticator, ls ListenerStore, subject string, tc *TokenClaims) (*httptest.ResponseRecorder, *Listener) {
	var seen *Listener
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l, ok := ListenerFrom(r.Context()); ok {
			seen = &l
		}
		ListenerMe(w, r)
	})
	rec := httptest.NewRecorder()
	a.authorizeListener(rec, httptest.NewRequest(http.MethodGet, "/api/v1/listen/me", nil), next, ls, subject, tc)
	return rec, seen
}

func TestListenerAdminPasses(t *testing.T) {
	a, forbidden := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		return verifiedUser(id, "admin@example.com", ""), nil
	})
	ls := &fakeListeners{}
	rec, l := serveListener(a, ls, "user_admin", nil)
	if rec.Code != 200 || l == nil || !l.Admin {
		t.Fatalf("admin = %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Data["admin"] != true || body.Data["email"] != "admin@example.com" || len(body.Data) != 4 {
		t.Errorf("me = %s", rec.Body)
	}
	if len(*forbidden) != 0 || ls.touches != 0 {
		t.Error("admin path touched the listener table or audit")
	}
}

func TestListenerUnknownAndRevoked403(t *testing.T) {
	a, forbidden := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		return verifiedUser(id, "guest@example.com", ListenerRole), nil
	})
	ls := &fakeListeners{}
	rec, _ := serveListener(a, ls, "user_g", nil)
	if code, msg := errorCode(t, rec); rec.Code != 403 || code != "forbidden" || msg != "this account is not invited to listen along" {
		t.Errorf("unknown = %d %s", rec.Code, rec.Body)
	}
	now := time.Now()
	uid := "user_g"
	ls.rows = []store.Listener{{ID: 1, Email: "guest@example.com", Status: store.ListenerRevoked, ClerkUserID: &uid, RevokedAt: &now}}
	if rec, _ := serveListener(a, ls, "user_g", nil); rec.Code != 403 {
		t.Errorf("revoked = %d", rec.Code)
	}
	if len(*forbidden) != 0 || len(ls.audit.calls) != 0 {
		t.Error("listener refusals must not be audited")
	}
	ls.err = errors.New("db down")
	if rec, _ := serveListener(a, ls, "user_g", nil); rec.Code != 500 {
		t.Errorf("db error = %d", rec.Code)
	}
}

func TestListenerAcceptOnce(t *testing.T) {
	a, _ := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		return verifiedUser(id, "Guest@Example.com", ListenerRole), nil
	})
	ls := &fakeListeners{rows: []store.Listener{{ID: 5, Email: "guest@example.com", Status: store.ListenerInvited}}}
	rec, l := serveListener(a, ls, "user_g", nil)
	if rec.Code != 200 || l == nil || l.Admin || l.ID != 5 {
		t.Fatalf("first = %d %s", rec.Code, rec.Body)
	}
	if ls.rows[0].Status != store.ListenerActive || *ls.rows[0].ClerkUserID != "user_g" {
		t.Errorf("row = %+v", ls.rows[0])
	}
	if c := ls.audit.calls; len(c) != 1 || c[0].action != "listener.accepted" || c[0].actor != "guest@example.com" ||
		c[0].detail["clerk_user_id"] != "user_g" || c[0].detail["id"] != float64(5) {
		t.Errorf("audit = %+v", c)
	}
	if ls.touches != 0 {
		t.Error("accept also touched")
	}
	// Second request: no new audit, no touch (seen just now).
	if rec, _ := serveListener(a, ls, "user_g", nil); rec.Code != 200 || len(ls.audit.calls) != 1 || ls.touches != 0 {
		t.Errorf("second = %d, audits %d, touches %d", rec.Code, len(ls.audit.calls), ls.touches)
	}
	// Another Clerk account with the same email cannot use the bound row.
	if rec, _ := serveListener(a, ls, "user_other", nil); rec.Code != 403 {
		t.Errorf("other account = %d", rec.Code)
	}
}

func TestListenerAcceptRace(t *testing.T) {
	a, _ := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		return verifiedUser(id, "guest@example.com", ListenerRole), nil
	})
	ls := &fakeListeners{rows: []store.Listener{{ID: 5, Email: "guest@example.com", Status: store.ListenerInvited}}}
	var gate sync.WaitGroup
	gate.Add(4)
	ls.beforeAccept = func() { gate.Done(); gate.Wait() } // all four read "invited" first
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range 4 {
		wg.Go(func() {
			rec, _ := serveListener(a, ls, "user_g", nil)
			codes[i] = rec.Code
		})
	}
	wg.Wait()
	for i, c := range codes {
		if c != 200 {
			t.Errorf("request %d = %d", i, c)
		}
	}
	if len(ls.audit.calls) != 1 {
		t.Errorf("accepted audited %d times", len(ls.audit.calls))
	}
}

func TestListenerTouch(t *testing.T) {
	a, _ := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		return verifiedUser(id, "guest@example.com", ListenerRole), nil
	})
	uid, old := "user_g", time.Now().Add(-10*time.Minute)
	ls := &fakeListeners{rows: []store.Listener{{ID: 1, Email: "guest@example.com", Status: store.ListenerActive,
		ClerkUserID: &uid, AcceptedAt: &old, LastSeenAt: &old}}}
	if rec, _ := serveListener(a, ls, "user_g", nil); rec.Code != 200 || ls.touches != 1 {
		t.Errorf("stale = %d touches %d", rec.Code, ls.touches)
	}
	if rec, _ := serveListener(a, ls, "user_g", nil); rec.Code != 200 || ls.touches != 1 {
		t.Errorf("fresh = %d touches %d", rec.Code, ls.touches)
	}
	ls.rows[0].LastSeenAt = nil
	if serveListener(a, ls, "user_g", nil); ls.touches != 2 {
		t.Errorf("never seen: touches %d", ls.touches)
	}
}

func TestListenerBanned(t *testing.T) {
	a, forbidden := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		u := verifiedUser(id, "guest@example.com", ListenerRole)
		u.Banned = true
		return u, nil
	})
	rec, _ := serveListener(a, &fakeListeners{}, "user_g", nil)
	if code, msg := errorCode(t, rec); rec.Code != 403 || code != "forbidden" || msg != "this account is banned or locked" {
		t.Errorf("banned = %d %s", rec.Code, rec.Body)
	}
	if len(*forbidden) != 0 {
		t.Error("listener refusal reached onForbidden")
	}
}

func TestListenerClaimsFallback(t *testing.T) {
	a, _ := newTestAuth(t, func(context.Context, string) (*clerk.User, error) {
		return nil, errors.New("dial tcp: connection refused")
	})
	uid := "user_g"
	ls := &fakeListeners{rows: []store.Listener{{ID: 1, Email: "guest@example.com", Status: store.ListenerActive, ClerkUserID: &uid}}}
	rec, l := serveListener(a, ls, "user_g", &TokenClaims{Role: ListenerRole, Email: "guest@example.com"})
	if rec.Code != 200 || l == nil || l.Admin {
		t.Errorf("fallback = %d %s", rec.Code, rec.Body)
	}
	// Clerk down and no claims: 503.
	if rec, _ := serveListener(a, ls, "user_g", nil); rec.Code != 503 {
		t.Errorf("no claims = %d", rec.Code)
	}
}

func TestListenerRoleNeverAdmin(t *testing.T) {
	a, _ := newTestAuth(t, func(_ context.Context, id string) (*clerk.User, error) {
		return verifiedUser(id, "guest@example.com", ListenerRole), nil
	})
	a.adminRole = ListenerRole // a misconfiguration config rejects; still not admin
	if a.allowed(User{ID: "user_g", Email: "guest@example.com", Role: ListenerRole}) {
		t.Fatal("listener role granted admin")
	}
	// The admin middleware refuses a listener.
	if rec, reached := serve(a, "user_g", nil); reached || rec.Code != 403 {
		t.Errorf("admin route = %d reached %v", rec.Code, reached)
	}
}

func TestListenerMiddlewareRequiresToken(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	h := a.ListenerMiddleware(&fakeListeners{})(http.HandlerFunc(ListenerMe))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/listen/me", nil))
	if rec.Code != 401 {
		t.Errorf("no token = %d %s", rec.Code, rec.Body)
	}
}
