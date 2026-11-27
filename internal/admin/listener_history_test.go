package admin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/store"
)

func TestListenerHistoryRoute(t *testing.T) {
	h := newListenersHarness()
	now := time.Now()
	h.db.rows = []store.Listener{{ID: 3, Email: "ann@example.com", Status: store.ListenerRevoked, RevokedAt: &now, InvitedBy: "a"}}
	h.db.history = map[int64][]store.ListenPlay{3: {
		{ID: 2, ListenedAt: now, Track: store.ListenPlayTrack{ID: "t2", Artists: []store.ListenPlayArtist{}}},
		{ID: 1, ListenedAt: now.Add(-time.Hour), Track: store.ListenPlayTrack{ID: "t1", Artists: []store.ListenPlayArtist{}}},
	}}

	// Revoked listeners keep their history; paged with next_cursor.
	code, out := h.do(t, "GET", "/admin/api/listeners/3/history?limit=1", ``)
	var page []store.ListenPlay
	_ = json.Unmarshal(out["data"], &page)
	if code != 200 || len(page) != 1 || page[0].ID != 2 || string(out["next_cursor"]) != `"1"` {
		t.Fatalf("page 1 = %d %s", code, out)
	}
	code, out = h.do(t, "GET", "/admin/api/listeners/3/history?limit=1&cursor=1", ``)
	if code != 200 || string(out["next_cursor"]) != "null" {
		t.Errorf("page 2 = %d %s", code, out)
	}
	// No history: an empty list, not null.
	h.db.rows = append(h.db.rows, store.Listener{ID: 4, Email: "bob@example.com", Status: store.ListenerActive, InvitedBy: "a"})
	if _, out := h.do(t, "GET", "/admin/api/listeners/4/history", ``); string(out["data"]) != "[]" {
		t.Errorf("empty = %s", out["data"])
	}

	for path, want := range map[string]struct {
		status int
		code   string
	}{
		"/admin/api/listeners/99/history":           {404, "not_found"},
		"/admin/api/listeners/x/history":            {404, "not_found"},
		"/admin/api/listeners/3/history?limit=0":    {400, "invalid_limit"},
		"/admin/api/listeners/3/history?limit=101":  {400, "invalid_limit"},
		"/admin/api/listeners/3/history?cursor=bad": {400, "invalid_cursor"},
	} {
		if code, out := h.do(t, "GET", path, ``); code != want.status || errCode(t, out) != want.code {
			t.Errorf("%s = %d %s", path, code, out)
		}
	}
	if len(h.audit.calls) != 0 {
		t.Error("history reads were audited")
	}
}
