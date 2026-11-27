package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

const (
	audioID1 = "4uLU6hMCjMI75M1A2tKUQC"
	audioID2 = "0VjIjW4GlUZAMYd2vXMi3b"
	audioID3 = "7qiZfU4dY1lWllzX7mPBI3"
)

type fakeAudioRows struct {
	rows  map[string]store.TrackAudio
	asked [][]string
	err   error
}

func (f *fakeAudioRows) TrackAudioByIDs(_ context.Context, ids []string) (map[string]store.TrackAudio, error) {
	f.asked = append(f.asked, ids)
	out := map[string]store.TrackAudio{}
	for _, id := range ids {
		if r, ok := f.rows[id]; ok {
			out[id] = r
		}
	}
	return out, f.err
}

type fakeChecker struct {
	found map[string]string
	err   error
	asked []string
}

func (f *fakeChecker) Find(_ context.Context, id string) (string, bool, error) {
	f.asked = append(f.asked, id)
	ext, ok := f.found[id]
	return ext, ok, f.err
}

func newStatusHarness(info ListenInfo, rows *fakeAudioRows, check AudioChecker) *listenersHarness {
	h := newListenersHarness()
	api := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), Listen: info, AudioCheck: check},
		audioStatusStore: rows, auditor: h.audit}
	r := chi.NewRouter()
	r.Route("/admin/api", api.mountListenAudio)
	h.router = r
	return h
}

type statusBody struct {
	Data map[string]struct {
		Available bool    `json:"available"`
		Ext       *string `json:"ext"`
		SizeBytes *int64  `json:"size_bytes"`
	} `json:"data"`
	ListenEnabled   bool `json:"listen_enabled"`
	AudioConfigured bool `json:"audio_configured"`
}

func decodeStatus(t *testing.T, out map[string]json.RawMessage) statusBody {
	t.Helper()
	raw, _ := json.Marshal(out)
	var b statusBody
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// manyIDs builds n distinct valid track IDs, comma-separated.
func manyIDs(n int) string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("a%020dz", i)
	}
	return strings.Join(ids, ",")
}

func TestAudioStatusRoute(t *testing.T) {
	rows := &fakeAudioRows{rows: map[string]store.TrackAudio{audioID2: {TrackID: audioID2, Ext: "mp3", SizeBytes: 1234}}}
	check := &fakeChecker{found: map[string]string{audioID1: "flac"}}
	h := newStatusHarness(ListenInfo{Enabled: true, AudioConfigured: true}, rows, check)

	code, out := h.do(t, "GET", "/admin/api/listen/audio/status?ids="+audioID1+","+audioID2+","+audioID3+","+audioID2, "")
	b := decodeStatus(t, out)
	if code != 200 || !b.ListenEnabled || !b.AudioConfigured || len(b.Data) != 3 {
		t.Fatalf("status = %d %s", code, out)
	}
	// The first id (the current song) has no row: the bucket answered.
	if d := b.Data[audioID1]; !d.Available || d.Ext == nil || *d.Ext != "flac" || d.SizeBytes != nil {
		t.Errorf("first = %+v", d)
	}
	if d := b.Data[audioID2]; !d.Available || *d.Ext != "mp3" || *d.SizeBytes != 1234 {
		t.Errorf("row = %+v", d)
	}
	if d := b.Data[audioID3]; d.Available || d.Ext != nil || d.SizeBytes != nil {
		t.Errorf("missing = %+v", d)
	}
	// One query (ids deduplicated); the bucket only for the first id.
	if len(rows.asked) != 1 || len(rows.asked[0]) != 3 || len(check.asked) != 1 || check.asked[0] != audioID1 {
		t.Errorf("asked rows %v, bucket %v", rows.asked, check.asked)
	}
	if strings.Contains(string(out["data"]), "http") {
		t.Errorf("a URL leaked: %s", out["data"])
	}
	// The first id has a row: no bucket check.
	check.asked = nil
	h.do(t, "GET", "/admin/api/listen/audio/status?ids="+audioID2+","+audioID1, "")
	if len(check.asked) != 0 {
		t.Errorf("bucket asked with a row: %v", check.asked)
	}
	// A bucket error is not fatal.
	check.err = errors.New("storage answered 500")
	if code, out := h.do(t, "GET", "/admin/api/listen/audio/status?ids="+audioID3, ""); code != 200 || decodeStatus(t, out).Data[audioID3].Available {
		t.Errorf("bucket error = %d %s", code, out)
	}

	for _, q := range []string{"", "?ids=", "?ids=nope", "?ids=" + audioID1 + ",x", "?ids=" + manyIDs(51)} {
		if code, out := h.do(t, "GET", "/admin/api/listen/audio/status"+q, ""); code != 400 || errCode(t, out) != "invalid_ids" {
			t.Errorf("%q = %d %s", q, code, out)
		}
	}
	if code, _ := h.do(t, "GET", "/admin/api/listen/audio/status?ids="+manyIDs(50), ""); code != 200 {
		t.Errorf("50 ids = %d", code)
	}
	if len(h.audit.calls) != 0 {
		t.Error("status reads were audited")
	}
}

func TestAudioStatusOff(t *testing.T) {
	rows := &fakeAudioRows{rows: map[string]store.TrackAudio{audioID1: {TrackID: audioID1, Ext: "mp3"}}}
	check := &fakeChecker{found: map[string]string{audioID1: "mp3"}}
	for _, info := range []ListenInfo{{Enabled: false, AudioConfigured: true}, {Enabled: true, AudioConfigured: false}} {
		h := newStatusHarness(info, rows, check)
		code, out := h.do(t, "GET", "/admin/api/listen/audio/status?ids="+audioID1, "")
		b := decodeStatus(t, out)
		if code != 200 || b.Data[audioID1].Available || b.ListenEnabled != info.Enabled || b.AudioConfigured != info.AudioConfigured {
			t.Errorf("%+v = %d %s", info, code, out)
		}
	}
	if len(rows.asked) != 0 || len(check.asked) != 0 {
		t.Errorf("storage asked while off: %v %v", rows.asked, check.asked)
	}
}

func TestOverviewAudio(t *testing.T) {
	rows := &fakeAudioRows{rows: map[string]store.TrackAudio{audioID1: {TrackID: audioID1, Ext: "mp3"}}}
	a := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), Listen: ListenInfo{Enabled: true, AudioConfigured: true}},
		audioStatusStore: rows}
	ctx := context.Background()
	np := func(id string) *domain.NowPlaying {
		return &domain.NowPlaying{Track: &domain.TrackView{Track: domain.Track{ID: id}}}
	}
	if v := a.overviewAudio(ctx, np(audioID1)); v == nil || !v.Available {
		t.Errorf("with file = %+v", v)
	}
	if v := a.overviewAudio(ctx, np(audioID2)); v == nil || v.Available {
		t.Errorf("without file = %+v", v)
	}
	if a.overviewAudio(ctx, nil) != nil || a.overviewAudio(ctx, &domain.NowPlaying{}) != nil {
		t.Error("nothing playing is not null")
	}
	rows.err = errors.New("db down")
	if a.overviewAudio(ctx, np(audioID1)) != nil {
		t.Error("error is not null")
	}
	rows.err = nil
	a.Listen.Enabled = false
	if a.overviewAudio(ctx, np(audioID1)) != nil {
		t.Error("listen off is not null")
	}
	// Storage off: available false, not null.
	a.Listen = ListenInfo{Enabled: true}
	if raw, _ := json.Marshal(a.overviewAudio(ctx, np(audioID1))); string(raw) != `{"available":false}` {
		t.Errorf("storage off json = %s", raw)
	}
}
