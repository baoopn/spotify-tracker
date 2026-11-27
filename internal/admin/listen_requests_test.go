package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// fakeRequests is an in-memory listen_requests table.
type fakeRequests struct {
	rows   []store.ListenRequest
	listed []string
}

func (f *fakeRequests) ListListenRequests(_ context.Context, status string) ([]store.ListenRequest, int64, error) {
	f.listed = append(f.listed, status)
	out := []store.ListenRequest{}
	for _, r := range f.rows {
		if status == "all" || r.Status == status {
			out = append(out, r)
		}
	}
	n, _ := f.PendingListenRequests(context.Background())
	return out, n, nil
}

func (f *fakeRequests) GetListenRequest(_ context.Context, id int64) (store.ListenRequest, error) {
	for _, r := range f.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return store.ListenRequest{}, domain.ErrNotFound
}

func (f *fakeRequests) DecideListenRequest(_ context.Context, id int64, status, by string) (store.ListenRequest, error) {
	for i, r := range f.rows {
		if r.ID != id {
			continue
		}
		if r.Status != store.RequestPending {
			return store.ListenRequest{}, store.ErrRequestNotPending
		}
		now := time.Now()
		f.rows[i].Status, f.rows[i].DecidedAt, f.rows[i].DecidedBy = status, &now, &by
		return f.rows[i], nil
	}
	return store.ListenRequest{}, domain.ErrNotFound
}

func (f *fakeRequests) PendingListenRequests(context.Context) (int64, error) {
	var n int64
	for _, r := range f.rows {
		if r.Status == store.RequestPending {
			n++
		}
	}
	return n, nil
}

func withRequests(h *listenersHarness) {
	now := time.Now()
	h.reqs.rows = []store.ListenRequest{
		{ID: 1, Name: "Ann", Email: "ann@example.com", Status: store.RequestPending, CreatedAt: now},
		{ID: 2, Name: "Old", Email: "old@example.com", Status: store.RequestDismissed, CreatedAt: now},
		{ID: 3, Name: "Bob", Email: "bob@example.com", Status: store.RequestPending, CreatedAt: now},
	}
}

func TestListRequestsAndBadge(t *testing.T) {
	h := newListenersHarness()
	withRequests(h)
	code, out := h.do(t, "GET", "/admin/api/listeners/requests", ``)
	var rows []map[string]any
	_ = json.Unmarshal(out["data"], &rows)
	if code != 200 || len(rows) != 2 || string(out["pending"]) != "2" || h.reqs.listed[0] != "pending" {
		t.Fatalf("pending list = %d %s", code, out)
	}
	for _, k := range []string{"id", "name", "email", "message", "status", "country", "created_at", "updated_at",
		"decided_at", "decided_by", "already_listener"} {
		if _, ok := rows[0][k]; !ok {
			t.Errorf("missing field %s", k)
		}
	}
	if _, out := h.do(t, "GET", "/admin/api/listeners/requests?status=all", ``); len(out["data"]) < 10 {
		t.Errorf("all = %s", out["data"])
	}
	if code, _ := h.do(t, "GET", "/admin/api/listeners/requests?status=invited", ``); code != 400 {
		t.Errorf("bad status = %d", code)
	}
	if _, out := h.do(t, "GET", "/admin/api/listeners", ``); string(out["pending_requests"]) != "2" {
		t.Errorf("badge = %s", out["pending_requests"])
	}
}

func TestInviteRequestReusesInvite(t *testing.T) {
	h := newListenersHarness()
	withRequests(h)
	code, out := h.do(t, "POST", "/admin/api/listeners/requests/1/invite", ``)
	if code != 201 || string(out["invitation_sent"]) != "true" {
		t.Fatalf("invite = %d %s", code, out)
	}
	if v := view(t, out["data"]); v.Email != "ann@example.com" || v.Status != "invited" {
		t.Errorf("listener = %+v", v)
	}
	var req store.ListenRequest
	_ = json.Unmarshal(out["request"], &req)
	if req.Status != store.RequestInvited || req.DecidedBy == nil || *req.DecidedBy != "admin@example.com" {
		t.Errorf("request = %+v", req)
	}
	// The same path as POST /listeners: a Clerk invitation and listener.invited.
	if len(h.clerk.invites) != 1 || len(h.db.rows) != 1 {
		t.Errorf("invites %v rows %d", h.clerk.invites, len(h.db.rows))
	}
	var actions []string
	for _, c := range h.audit.calls {
		actions = append(actions, c.action)
	}
	if len(actions) != 2 || actions[0] != "listener.invited" || actions[1] != "listener.request_invited" {
		t.Errorf("audit = %v", actions)
	}

	// Errors: already handled, unknown, and the invite errors (already a listener).
	for path, want := range map[string]struct {
		status int
		code   string
	}{
		"/admin/api/listeners/requests/1/invite":  {409, "not_pending"},
		"/admin/api/listeners/requests/2/invite":  {409, "not_pending"},
		"/admin/api/listeners/requests/99/invite": {404, "not_found"},
		"/admin/api/listeners/requests/x/invite":  {404, "not_found"},
	} {
		if code, out := h.do(t, "POST", path, ``); code != want.status || errCode(t, out) != want.code {
			t.Errorf("%s = %d %s", path, code, out)
		}
	}
	h.db.rows = append(h.db.rows, store.Listener{ID: 50, Email: "bob@example.com", Status: store.ListenerActive, InvitedBy: "a"})
	if code, out := h.do(t, "POST", "/admin/api/listeners/requests/3/invite", ``); code != 409 || errCode(t, out) != "listener_exists" {
		t.Errorf("already a listener = %d %s", code, out)
	}
	if r, _ := h.reqs.GetListenRequest(context.Background(), 3); r.Status != store.RequestPending {
		t.Errorf("failed invite changed the request: %+v", r)
	}
}

func TestDismissRequest(t *testing.T) {
	h := newListenersHarness()
	withRequests(h)
	code, out := h.do(t, "POST", "/admin/api/listeners/requests/3/dismiss", ``)
	var req store.ListenRequest
	_ = json.Unmarshal(out["request"], &req)
	if code != 200 || req.Status != store.RequestDismissed {
		t.Fatalf("dismiss = %d %s", code, out)
	}
	if last := h.audit.calls[len(h.audit.calls)-1]; last.action != "listener.request_dismissed" {
		t.Errorf("audit = %+v", last)
	}
	if code, out := h.do(t, "POST", "/admin/api/listeners/requests/3/dismiss", ``); code != 409 || errCode(t, out) != "not_pending" {
		t.Errorf("again = %d %s", code, out)
	}
	if code, _ := h.do(t, "POST", "/admin/api/listeners/requests/42/dismiss", ``); code != 404 {
		t.Errorf("unknown = %d", code)
	}
	if len(h.clerk.invites) != 0 {
		t.Error("dismiss called Clerk")
	}
}
