package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/listen"
)

type fakePresence struct{ got []listen.Presence }

func (f *fakePresence) Touch(_ context.Context, p listen.Presence) error {
	f.got = append(f.got, p)
	return nil
}

func TestListenPresence(t *testing.T) {
	pres := &fakePresence{}
	o := &ListenOptions{Enabled: true, Auth: allowAll, Me: me, Listener: asListener(7), Presence: pres}
	h := listenRouter(o, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/listen/presence",
		strings.NewReader(`{"state":"following","track_id":"`+trackID+`"}`))
	req.Header.Set("CF-IPCountry", "vn")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/130.0 Safari/537.36")
	req.RemoteAddr = "203.0.113.9:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("presence = %d %s", rec.Code, rec.Body)
	}
	if len(pres.got) != 1 {
		t.Fatalf("got = %+v", pres.got)
	}
	p := pres.got[0]
	if p.ListenerID != 7 || p.Email != "guest@example.com" || p.State != "following" || *p.TrackID != trackID ||
		p.Country != "VN" || p.Device != "desktop" || p.Browser != "Chrome" {
		t.Errorf("presence = %+v", p)
	}
	if rec := post(h, "/api/v1/listen/presence", `{"state":"idle","track_id":null}`); rec.Code != 204 || pres.got[1].TrackID != nil {
		t.Errorf("idle = %d %+v", rec.Code, pres.got)
	}

	for _, c := range []struct{ body, field string }{
		{`{"state":"away"}`, "state"},
		{`{"state":"listening","track_id":"nope"}`, "track_id"},
	} {
		rec := post(h, "/api/v1/listen/presence", c.body)
		if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"`+c.field+`"`) {
			t.Errorf("%s = %d %s", c.body, rec.Code, rec.Body)
		}
	}
	if rec := post(h, "/api/v1/listen/presence", `{"state":"idle","ip":"x"}`); rec.Code != 400 {
		t.Errorf("unknown field = %d", rec.Code)
	}
	// Admin previewing: 204, nothing stored.
	admin := &ListenOptions{Enabled: true, Auth: allowAll, Me: me, Listener: asListener(0), Presence: pres}
	if rec := post(listenRouter(admin, nil), "/api/v1/listen/presence", `{"state":"idle"}`); rec.Code != 204 || len(pres.got) != 2 {
		t.Errorf("admin = %d, stored %d", rec.Code, len(pres.got))
	}
}

func TestProgressRefreshesPresence(t *testing.T) {
	pres := &fakePresence{}
	o := &ListenOptions{Enabled: true, Auth: allowAll, Me: me, Listener: asListener(7), Plays: &fakePlays{}, Presence: pres}
	snap := snapshot(true, ms(60_000), lyricsNow.Add(-2*time.Second), trackID, 200_000)
	if rec := post(listenRouter(o, snap), "/api/v1/listen/progress", `{"track_id":"`+trackID+`","heard_ms":15000}`); rec.Code != 204 {
		t.Fatalf("progress = %d", rec.Code)
	}
	if len(pres.got) != 1 || pres.got[0].State != listen.StateListening || *pres.got[0].TrackID != trackID {
		t.Errorf("presence = %+v", pres.got)
	}
}
