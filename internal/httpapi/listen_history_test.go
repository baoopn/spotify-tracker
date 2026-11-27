package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/store"
)

type progressCall struct {
	listener     int64
	track        string
	delta, durMS int64
}

type fakePlays struct {
	calls   []progressCall
	history map[int64][]store.ListenPlay
	cursors []string
}

func (f *fakePlays) RecordListenerProgress(_ context.Context, id int64, track string, delta, dur int64) error {
	f.calls = append(f.calls, progressCall{id, track, delta, dur})
	return nil
}

func (f *fakePlays) ListenerHistory(_ context.Context, id int64, limit int, cursor string) ([]store.ListenPlay, *string, error) {
	f.cursors = append(f.cursors, cursor)
	if cursor == "bad" {
		return nil, nil, store.ErrBadCursor
	}
	all := f.history[id]
	if len(all) > limit {
		next := "next"
		return all[:limit], &next, nil
	}
	return all, nil, nil
}

// asListener returns a Listener func for listener id (0 = admin preview).
func asListener(id int64) func(context.Context) (ListenerIdentity, bool) {
	return func(context.Context) (ListenerIdentity, bool) {
		return ListenerIdentity{ID: id, Email: "guest@example.com"}, id > 0
	}
}

func post(h http.Handler, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestListenProgress(t *testing.T) {
	fresh := lyricsNow.Add(-2 * time.Second)
	playing := snapshot(true, ms(60_000), fresh, trackID, 200_000)
	plays := &fakePlays{}
	o := &ListenOptions{Enabled: true, Auth: allowAll, Me: me, Listener: asListener(7), Plays: plays}
	h := listenRouter(o, playing)

	body := `{"track_id":"` + trackID + `","heard_ms":15000}`
	if rec := post(h, "/api/v1/listen/progress", body); rec.Code != 204 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("progress = %d %s", rec.Code, rec.Body)
	}
	if len(plays.calls) != 1 || plays.calls[0] != (progressCall{7, trackID, 15000, 200_000}) {
		t.Errorf("calls = %+v", plays.calls)
	}

	for _, c := range []struct{ body, field string }{
		{`{"track_id":"short","heard_ms":1000}`, "track_id"},
		{`{"track_id":"` + trackID + `","heard_ms":0}`, "heard_ms"},
		{`{"track_id":"` + trackID + `","heard_ms":20001}`, "heard_ms"},
		{`{"heard_ms":-5}`, "track_id"},
	} {
		rec := post(h, "/api/v1/listen/progress", c.body)
		if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"validation_failed"`) || !strings.Contains(rec.Body.String(), `"`+c.field+`"`) {
			t.Errorf("%s = %d %s", c.body, rec.Code, rec.Body)
		}
	}
	if rec := post(h, "/api/v1/listen/progress", `{"track_id":1}`); rec.Code != 400 {
		t.Errorf("bad json = %d", rec.Code)
	}

	// Not the current track, idle, and an admin previewing: 204, nothing stored.
	other := `{"track_id":"0000000000000000000000","heard_ms":1000}`
	post(h, "/api/v1/listen/progress", other)
	post(listenRouter(o, []byte("null")), "/api/v1/listen/progress", body)
	admin := &ListenOptions{Enabled: true, Auth: allowAll, Me: me, Listener: asListener(0), Plays: plays}
	if rec := post(listenRouter(admin, playing), "/api/v1/listen/progress", body); rec.Code != 204 {
		t.Errorf("admin = %d", rec.Code)
	}
	if len(plays.calls) != 1 {
		t.Errorf("stored %d reports, want 1", len(plays.calls))
	}
}

func TestListenHistory(t *testing.T) {
	plays := &fakePlays{history: map[int64][]store.ListenPlay{
		7: {{ID: 3, Track: store.ListenPlayTrack{ID: "a", Artists: []store.ListenPlayArtist{}}}, {ID: 2}, {ID: 1}},
		8: {{ID: 9}},
	}}
	o := &ListenOptions{Enabled: true, Auth: allowAll, Me: me, Listener: asListener(7), Plays: plays}
	h := listenRouter(o, nil)
	rec, out := get(t, h, "/api/v1/listen/history?limit=2&cursor=abc")
	if rec.Code != 200 || string(out["next_cursor"]) != `"next"` || !strings.Contains(string(out["data"]), `"id":3`) ||
		strings.Contains(string(out["data"]), `"id":9`) {
		t.Errorf("history = %d %s", rec.Code, rec.Body)
	}
	if plays.cursors[0] != "abc" {
		t.Errorf("cursor = %v", plays.cursors)
	}
	for path, code := range map[string]int{
		"/api/v1/listen/history?limit=0":    400,
		"/api/v1/listen/history?limit=101":  400,
		"/api/v1/listen/history?cursor=bad": 400,
	} {
		if rec, _ := get(t, h, path); rec.Code != code {
			t.Errorf("%s = %d", path, rec.Code)
		}
	}
	// Admin previewing: an empty page.
	admin := &ListenOptions{Enabled: true, Auth: allowAll, Me: me, Listener: asListener(0), Plays: plays}
	if _, out := get(t, listenRouter(admin, nil), "/api/v1/listen/history"); string(out["data"]) != "[]" || string(out["next_cursor"]) != "null" {
		t.Errorf("admin = %s", out)
	}
	// Disabled: 404 like the other listener routes.
	off := &ListenOptions{Enabled: false, Auth: allowAll, Me: me, Listener: asListener(7), Plays: plays}
	if rec, _ := get(t, listenRouter(off, nil), "/api/v1/listen/history"); rec.Code != 404 {
		t.Errorf("disabled = %d", rec.Code)
	}
	if rec := post(listenRouter(off, nil), "/api/v1/listen/progress", `{}`); rec.Code != 404 {
		t.Errorf("disabled progress = %d", rec.Code)
	}
}
