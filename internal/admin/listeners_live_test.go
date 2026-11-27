package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/store"
)

type fakePresence struct {
	list    []listen.Presence
	deleted []int64
}

func (f *fakePresence) List(context.Context) ([]listen.Presence, error) { return f.list, nil }

func (f *fakePresence) Delete(_ context.Context, id int64) error {
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeLive struct {
	statuses map[int64]string
	tracks   map[string]store.LiveTrack
	asked    [][]int64
}

func (f *fakeLive) ListenerStatuses(_ context.Context, ids []int64) (map[int64]string, error) {
	f.asked = append(f.asked, ids)
	return f.statuses, nil
}

func (f *fakeLive) LiveTracks(_ context.Context, ids []string) (map[string]store.LiveTrack, error) {
	out := map[string]store.LiveTrack{}
	for _, id := range ids {
		if t, ok := f.tracks[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

func TestListenersLive(t *testing.T) {
	h := newListenersHarness()
	t0 := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	tr, gone := "4uLU6hMCjMI75M1A2tKUQC", "0000000000000000000000"
	pres := &fakePresence{list: []listen.Presence{
		{ListenerID: 1, Email: "idle@example.com", State: listen.StateIdle, Since: t0},
		{ListenerID: 2, Email: "late@example.com", State: listen.StateListening, TrackID: &tr, Country: "VN", Since: t0.Add(time.Minute)},
		{ListenerID: 3, Email: "early@example.com", State: listen.StateListening, TrackID: &tr, Since: t0},
		{ListenerID: 4, Email: "follow@example.com", State: listen.StateFollowing, TrackID: &gone, Since: t0},
		{ListenerID: 5, Email: "revoked@example.com", State: listen.StateListening, Since: t0},
		{ListenerID: 6, Email: "deleted@example.com", State: listen.StateListening, Since: t0},
	}}
	live := &fakeLive{
		statuses: map[int64]string{1: "active", 2: "active", 3: "invited", 4: "active", 5: "revoked"},
		tracks:   map[string]store.LiveTrack{tr: {ID: tr, Name: "Song", Artists: []store.ListenPlayArtist{{ID: "a", Name: "Artist"}}}},
	}
	api := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), Presence: pres}, listeners: h.db, clerkInv: h.clerk,
		auditor: h.audit, liveLookup: live}
	r := chi.NewRouter()
	r.Route("/admin/api", api.mountListeners)
	h.router = r

	code, out := h.do(t, "GET", "/admin/api/listeners/live", ``)
	var rows []map[string]any
	if err := json.Unmarshal(out["data"], &rows); err != nil || code != 200 {
		t.Fatalf("live = %d %s", code, out)
	}
	var emails []string
	for _, r := range rows {
		emails = append(emails, r["email"].(string))
	}
	want := []string{"early@example.com", "late@example.com", "follow@example.com", "idle@example.com"}
	if len(emails) != len(want) {
		t.Fatalf("emails = %v", emails)
	}
	for i := range want {
		if emails[i] != want[i] {
			t.Errorf("order = %v, want %v", emails, want)
			break
		}
	}
	if len(live.asked) != 1 || len(live.asked[0]) != 6 {
		t.Errorf("statuses looked up %v (want one query for all)", live.asked)
	}
	late := rows[1]
	track, _ := late["track"].(map[string]any)
	if late["country"] != "VN" || track["name"] != "Song" || len(track["artists"].([]any)) != 1 {
		t.Errorf("late = %v", late)
	}
	if rows[0]["country"] != nil || rows[3]["track"] != nil {
		t.Errorf("nulls: %v / %v", rows[0]["country"], rows[3]["track"])
	}
	if ft, _ := rows[2]["track"].(map[string]any); ft["id"] != gone || ft["name"] != "" {
		t.Errorf("unknown track = %v", rows[2]["track"])
	}
	for _, k := range []string{"listener_id", "email", "state", "country", "device", "browser", "since", "updated_at", "track"} {
		if _, ok := rows[0][k]; !ok {
			t.Errorf("missing field %s", k)
		}
	}

	// Revoking drops the presence key.
	h.db.rows = []store.Listener{{ID: 2, Email: "late@example.com", Status: store.ListenerActive, InvitedBy: "a"}}
	if code, _ := h.do(t, "POST", "/admin/api/listeners/2/revoke", ``); code != 200 || len(pres.deleted) != 1 || pres.deleted[0] != 2 {
		t.Errorf("revoke = %d, deleted %v", code, pres.deleted)
	}

	// No presence store: an empty list.
	api.Presence = nil
	if _, out := h.do(t, "GET", "/admin/api/listeners/live", ``); string(out["data"]) != "[]" {
		t.Errorf("no presence = %s", out["data"])
	}
}
