package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/store"
)

type listenersHarness struct {
	db     *fakeListeners
	clerk  *fakeClerk
	audit  *fakeAuditor
	router chi.Router
}

func newListenersHarness() *listenersHarness {
	h := &listenersHarness{db: &fakeListeners{}, clerk: &fakeClerk{users: map[string]string{}, roles: map[string]string{}},
		audit: &fakeAuditor{}}
	api := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler),
		Listen: ListenInfo{Enabled: true, AudioConfigured: true, URL: "https://listening.example.com/listen"}},
		listeners: h.db, clerkInv: h.clerk, auditor: h.audit}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := User{ID: "admin_1", Email: "admin@example.com"}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
		})
	})
	r.Route("/admin/api", api.mountListeners)
	h.router = r
	return h
}

func (h *listenersHarness) do(t *testing.T, method, path, body string) (int, map[string]json.RawMessage) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: bad body %q", method, path, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	return rec.Code, out
}

func view(t *testing.T, raw json.RawMessage) listenerView {
	t.Helper()
	var v listenerView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestInviteValidation(t *testing.T) {
	h := newListenersHarness()
	for _, email := range []string{"", "nope", "Ann <ann@example.com>", "a@b@c", strings.Repeat("a", 250) + "@x.io", " @x"} {
		body, _ := json.Marshal(map[string]string{"email": email})
		code, out := h.do(t, "POST", "/admin/api/listeners", string(body))
		var e apiError
		_ = json.Unmarshal(out["error"], &e)
		if code != 422 || e.Code != "validation_failed" || e.Fields["email"] != "must be a valid email address" {
			t.Errorf("%q: %d %+v", email, code, e)
		}
	}
	if code, out := h.do(t, "POST", "/admin/api/listeners", `{"email":1}`); code != 400 || errCode(t, out) != "invalid_request" {
		t.Errorf("bad json: %d", code)
	}
	if len(h.clerk.invites) != 0 {
		t.Error("Clerk called for invalid input")
	}
}

func TestInviteNewPerson(t *testing.T) {
	h := newListenersHarness()
	code, out := h.do(t, "POST", "/admin/api/listeners", `{"email":"  Ann@Example.COM "}`)
	if code != 201 || string(out["invitation_sent"]) != "true" {
		t.Fatalf("invite = %d %s", code, out)
	}
	v := view(t, out["data"])
	if v.Email != "ann@example.com" || v.Status != "invited" || v.HasAccount || v.InvitedBy != "admin@example.com" {
		t.Errorf("view = %+v", v)
	}
	if len(h.clerk.invites) != 1 || h.clerk.invites[0] != "ann@example.com https://listening.example.com/listen" {
		t.Errorf("invites = %v", h.clerk.invites)
	}
	if c := h.audit.calls; len(c) != 1 || c[0].action != "listener.invited" || c[0].detail["existing_account"] != false ||
		c[0].detail["email"] != "ann@example.com" || c[0].actor != "admin@example.com" {
		t.Errorf("audit = %+v", c)
	}

	// The same email again: 409 before any Clerk call.
	code, out = h.do(t, "POST", "/admin/api/listeners", `{"email":"ann@example.com"}`)
	if code != 409 || errCode(t, out) != "listener_exists" || len(h.clerk.invites) != 1 {
		t.Errorf("duplicate = %d %s, invites %d", code, out, len(h.clerk.invites))
	}
}

func TestInviteExistingAccount(t *testing.T) {
	h := newListenersHarness()
	h.clerk.users["bob@example.com"] = "user_b"
	code, out := h.do(t, "POST", "/admin/api/listeners", `{"email":"bob@example.com"}`)
	if code != 201 || string(out["invitation_sent"]) != "false" {
		t.Fatalf("invite = %d %s", code, out)
	}
	if v := view(t, out["data"]); !v.HasAccount || v.ClerkUserID == nil || *v.ClerkUserID != "user_b" {
		t.Errorf("view = %+v", v)
	}
	if len(h.clerk.invites) != 0 || len(h.clerk.setRoles) != 1 || h.clerk.setRoles[0] != "user_b=listener" {
		t.Errorf("clerk = %+v", h.clerk)
	}
	if h.audit.calls[0].detail["existing_account"] != true {
		t.Errorf("audit = %+v", h.audit.calls)
	}

	// An admin's role is never touched.
	h.clerk.users["root@example.com"], h.clerk.roles["user_r"] = "user_r", "admin"
	if code, _ := h.do(t, "POST", "/admin/api/listeners", `{"email":"root@example.com"}`); code != 201 || len(h.clerk.setRoles) != 1 {
		t.Errorf("admin invite = %d, roles %v", code, h.clerk.setRoles)
	}
}

func TestInviteClerkErrors(t *testing.T) {
	for _, c := range []struct {
		name     string
		err      error
		status   int
		code     string
		contains string
	}{
		{"network", errors.New("dial tcp"), 503, "clerk_unavailable", ""},
		{"5xx", &clerk.APIErrorResponse{HTTPStatusCode: 502}, 503, "clerk_unavailable", ""},
		{"429", &clerk.APIErrorResponse{HTTPStatusCode: 429}, 503, "clerk_unavailable", ""},
		{"4xx", &clerk.APIErrorResponse{HTTPStatusCode: 422, Errors: []clerk.Error{{Message: "short", LongMessage: "email address is blocked"}}},
			409, "clerk_rejected", "email address is blocked"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newListenersHarness()
			h.clerk.inviteErr = c.err
			code, out := h.do(t, "POST", "/admin/api/listeners", `{"email":"ann@example.com"}`)
			if code != c.status || errCode(t, out) != c.code || !strings.Contains(string(out["error"]), c.contains) {
				t.Errorf("got %d %s", code, out)
			}
			if len(h.db.rows) != 0 || len(h.audit.calls) != 0 {
				t.Error("row or audit written after a Clerk failure")
			}
		})
	}
	h := newListenersHarness()
	h.clerk.findErr = errors.New("timeout")
	if code, out := h.do(t, "POST", "/admin/api/listeners", `{"email":"ann@example.com"}`); code != 503 || errCode(t, out) != "clerk_unavailable" {
		t.Errorf("find error = %d", code)
	}
}

func TestInviteInsertConflictRevokesInvitation(t *testing.T) {
	h := newListenersHarness()
	// A row appears between the duplicate check and the insert.
	h.clerk.inviteErr = nil
	orig := h.clerk
	racing := &racingClerk{fakeClerk: orig, db: h.db}
	api := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), Listen: ListenInfo{URL: "u"}},
		listeners: h.db, clerkInv: racing, auditor: h.audit}
	r := chi.NewRouter()
	r.Route("/admin/api", api.mountListeners)
	h.router = r
	code, out := h.do(t, "POST", "/admin/api/listeners", `{"email":"ann@example.com"}`)
	if code != 409 || errCode(t, out) != "listener_exists" {
		t.Fatalf("got %d %s", code, out)
	}
	if len(orig.revoked) != 1 || orig.revoked[0] != "inv_1" {
		t.Errorf("unused invitation not revoked: %v", orig.revoked)
	}
}

// racingClerk inserts a row for the email while the invitation is sent.
type racingClerk struct {
	*fakeClerk
	db *fakeListeners
}

func (c *racingClerk) Invite(ctx context.Context, email, redirect string) (string, error) {
	_, _ = c.db.CreateListener(ctx, store.NewListener{Email: email, InvitedBy: "other"})
	return c.fakeClerk.Invite(ctx, email, redirect)
}

func TestResendListener(t *testing.T) {
	h := newListenersHarness()
	h.do(t, "POST", "/admin/api/listeners", `{"email":"ann@example.com"}`)
	code, out := h.do(t, "POST", "/admin/api/listeners/1/resend", ``)
	if code != 200 {
		t.Fatalf("resend = %d %s", code, out)
	}
	if v := view(t, out["data"]); v.ID != 1 || v.Status != "invited" {
		t.Errorf("view = %+v", v)
	}
	if h.db.rows[0].ClerkInvitationID == nil || *h.db.rows[0].ClerkInvitationID != "inv_2" ||
		len(h.clerk.revoked) != 1 || h.clerk.revoked[0] != "inv_1" {
		t.Errorf("row %+v revoked %v", h.db.rows[0], h.clerk.revoked)
	}
	if last := h.audit.calls[len(h.audit.calls)-1]; last.action != "listener.resent" || last.detail["email"] != "ann@example.com" {
		t.Errorf("audit = %+v", last)
	}

	// Errors.
	h.clerk.users["bob@example.com"] = "user_b"
	h.do(t, "POST", "/admin/api/listeners", `{"email":"bob@example.com"}`)
	for path, want := range map[string]struct {
		status int
		code   string
	}{
		"/admin/api/listeners/2/resend":   {409, "no_invitation"},
		"/admin/api/listeners/99/resend":  {404, "not_found"},
		"/admin/api/listeners/0/resend":   {404, "not_found"},
		"/admin/api/listeners/-1/resend":  {404, "not_found"},
		"/admin/api/listeners/abc/resend": {404, "not_found"},
	} {
		if code, out := h.do(t, "POST", path, ``); code != want.status || errCode(t, out) != want.code {
			t.Errorf("%s = %d %s", path, code, out)
		}
	}
	h.do(t, "POST", "/admin/api/listeners/1/revoke", ``)
	if code, out := h.do(t, "POST", "/admin/api/listeners/1/resend", ``); code != 409 || errCode(t, out) != "not_pending" {
		t.Errorf("revoked resend = %d %s", code, out)
	}
	h.do(t, "POST", "/admin/api/listeners", `{"email":"cy@example.com"}`)
	h.clerk.inviteErr = errors.New("down")
	if code, out := h.do(t, "POST", "/admin/api/listeners/3/resend", ``); code != 503 || errCode(t, out) != "clerk_unavailable" {
		t.Errorf("clerk down resend = %d %s", code, out)
	}
}

func TestRevokeListener(t *testing.T) {
	h := newListenersHarness()
	h.do(t, "POST", "/admin/api/listeners", `{"email":"ann@example.com"}`) // pending invitation
	h.clerk.users["bob@example.com"] = "user_b"
	h.do(t, "POST", "/admin/api/listeners", `{"email":"bob@example.com"}`) // existing account, role listener
	h.clerk.roles["user_b"] = "listener"
	h.clerk.users["root@example.com"], h.clerk.roles["user_r"] = "user_r", "admin"
	h.do(t, "POST", "/admin/api/listeners", `{"email":"root@example.com"}`)

	code, out := h.do(t, "POST", "/admin/api/listeners/1/revoke", ``)
	if code != 200 || string(out["clerk_cleanup"]) != `"ok"` || view(t, out["data"]).Status != "revoked" {
		t.Fatalf("revoke 1 = %d %s", code, out)
	}
	if len(h.clerk.revoked) != 1 || h.clerk.revoked[0] != "inv_1" {
		t.Errorf("invitation not revoked: %v", h.clerk.revoked)
	}
	h.clerk.setRoles = nil
	if code, out = h.do(t, "POST", "/admin/api/listeners/2/revoke", ``); code != 200 || string(out["clerk_cleanup"]) != `"ok"` {
		t.Errorf("revoke 2 = %d %s", code, out)
	}
	if len(h.clerk.setRoles) != 1 || h.clerk.setRoles[0] != "user_b=<nil>" {
		t.Errorf("role not cleared: %v", h.clerk.setRoles)
	}
	// An admin's role stays.
	if code, _ = h.do(t, "POST", "/admin/api/listeners/3/revoke", ``); code != 200 || len(h.clerk.setRoles) != 1 {
		t.Errorf("admin role touched: %v", h.clerk.setRoles)
	}
	last := h.audit.calls[len(h.audit.calls)-1]
	if last.action != "listener.revoked" || last.detail["clerk_cleanup"] != "ok" || last.detail["email"] != "root@example.com" {
		t.Errorf("audit = %+v", last)
	}

	if code, out = h.do(t, "POST", "/admin/api/listeners/1/revoke", ``); code != 409 || errCode(t, out) != "already_revoked" {
		t.Errorf("again = %d %s", code, out)
	}
	if code, out = h.do(t, "POST", "/admin/api/listeners/42/revoke", ``); code != 404 || errCode(t, out) != "not_found" {
		t.Errorf("unknown = %d %s", code, out)
	}

	// Clerk failures never fail the revoke.
	h.clerk.users["dee@example.com"] = "user_d"
	h.do(t, "POST", "/admin/api/listeners", `{"email":"dee@example.com"}`)
	h.clerk.roleErr = errors.New("down")
	code, out = h.do(t, "POST", "/admin/api/listeners/4/revoke", ``)
	if code != 200 || string(out["clerk_cleanup"]) != `"failed"` || h.db.rows[3].Status != store.ListenerRevoked {
		t.Errorf("clerk down revoke = %d %s", code, out)
	}
}

func TestRevokeCleanupSkipped(t *testing.T) {
	h := newListenersHarness()
	h.db.rows = []store.Listener{{ID: 7, Email: "x@example.com", Status: store.ListenerInvited, InvitedBy: "a"}}
	code, out := h.do(t, "POST", "/admin/api/listeners/7/revoke", ``)
	if code != 200 || string(out["clerk_cleanup"]) != `"skipped"` {
		t.Errorf("got %d %s", code, out)
	}
}

func TestListListeners(t *testing.T) {
	h := newListenersHarness()
	code, out := h.do(t, "GET", "/admin/api/listeners", ``)
	if code != 200 || string(out["data"]) != "[]" || string(out["listen_enabled"]) != "true" ||
		string(out["audio_configured"]) != "true" || string(out["listen_url"]) != `"https://listening.example.com/listen"` {
		t.Errorf("empty list = %d %s", code, out)
	}
	h.do(t, "POST", "/admin/api/listeners", `{"email":"ann@example.com"}`)
	_, out = h.do(t, "GET", "/admin/api/listeners", ``)
	var rows []map[string]any
	_ = json.Unmarshal(out["data"], &rows)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	for _, k := range []string{"id", "email", "status", "clerk_user_id", "has_account", "invited_by", "invited_at",
		"accepted_at", "revoked_at", "last_seen_at"} {
		if _, ok := rows[0][k]; !ok {
			t.Errorf("missing field %s", k)
		}
	}
	h.db.err = errors.New("db down")
	if code, out := h.do(t, "GET", "/admin/api/listeners", ``); code != 500 || errCode(t, out) != "internal" {
		t.Errorf("db error = %d", code)
	}
}
