package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

const plTrack = "4uLU6hMCjMI75M1A2tKUQC"

// fakePlayer records calls and returns err for every command.
type fakePlayer struct {
	calls []string
	err   error
	state *spotify.PlaybackState
	devs  []spotify.Device
	queue *spotify.PlayerQueue
	qErr  error
}

func (f *fakePlayer) rec(format string, args ...any) error {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	return f.err
}

func (f *fakePlayer) PlaybackState(context.Context) (*spotify.PlaybackState, error) {
	return f.state, f.err
}
func (f *fakePlayer) Devices(context.Context) ([]spotify.Device, error) { return f.devs, nil }
func (f *fakePlayer) PlayerQueue(context.Context, int) (*spotify.PlayerQueue, error) {
	return f.queue, f.qErr
}
func (f *fakePlayer) SearchTracks(_ context.Context, q string, limit int) ([]spotify.PlayerTrack, error) {
	_ = f.rec("search %s %d", q, limit)
	return []spotify.PlayerTrack{{ID: plTrack, Name: "Found"}}, f.err
}
func (f *fakePlayer) Play(_ context.Context, o spotify.PlayOptions) error {
	return f.rec("play %s %v %s", o.DeviceID, o.URIs, o.ContextURI)
}
func (f *fakePlayer) Pause(_ context.Context, d string) error    { return f.rec("pause %s", d) }
func (f *fakePlayer) Next(_ context.Context, d string) error     { return f.rec("next %s", d) }
func (f *fakePlayer) Previous(_ context.Context, d string) error { return f.rec("previous %s", d) }
func (f *fakePlayer) Seek(_ context.Context, ms int64, d string) error {
	return f.rec("seek %d %s", ms, d)
}
func (f *fakePlayer) SetVolume(_ context.Context, v int, d string) error {
	return f.rec("volume %d %s", v, d)
}
func (f *fakePlayer) SetShuffle(_ context.Context, on bool, d string) error {
	return f.rec("shuffle %v %s", on, d)
}
func (f *fakePlayer) SetRepeat(_ context.Context, m, d string) error {
	return f.rec("repeat %s %s", m, d)
}
func (f *fakePlayer) Transfer(_ context.Context, d string, play bool) error {
	return f.rec("transfer %s %v", d, play)
}
func (f *fakePlayer) AddToQueue(_ context.Context, uri, d string) error {
	return f.rec("queue %s %s", uri, d)
}

type playerHarness struct {
	pl     *fakePlayer
	audit  *fakeAuditor
	sp     settings.Spotify
	spErr  error
	router http.Handler
}

func newPlayerHarness(t *testing.T) *playerHarness {
	t.Helper()
	h := &playerHarness{pl: &fakePlayer{}, audit: &fakeAuditor{},
		sp: settings.Spotify{RefreshToken: "rt", Scopes: []string{"user-read-playback-state", spotify.PlaybackControlScope}}}
	api := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler)}, player: h.pl, auditor: h.audit,
		spotifyAuth: func(context.Context) (settings.Spotify, error) { return h.sp, h.spErr }}
	r := chi.NewRouter()
	r.Route("/admin/api", api.mountPlayer)
	h.router = r
	return h
}

func (h *playerHarness) do(t *testing.T, method, path, body string) (int, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, User{Email: "admin@example.com"}))
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: invalid JSON %q", method, path, rec.Body)
	}
	return rec.Code, out
}

func TestPlayerCommandsCallAndAudit(t *testing.T) {
	for _, tc := range []struct{ path, body, call, action string }{
		{"/player/play", `{}`, "play  [] ", "player.play"},
		{"/player/play", `{"uris":["` + plTrack + `"],"device_id":"d1"}`, "play d1 [" + plTrack + "] ", "player.play"},
		{"/player/pause", ``, "pause ", "player.pause"},
		{"/player/next", `{"device_id":"d1"}`, "next d1", "player.next"},
		{"/player/previous", `{}`, "previous ", "player.previous"},
		{"/player/seek", `{"position_ms":1500}`, "seek 1500 ", "player.seek"},
		{"/player/volume", `{"volume_percent":0}`, "volume 0 ", "player.volume"},
		{"/player/shuffle", `{"state":false}`, "shuffle false ", "player.shuffle"},
		{"/player/repeat", `{"state":"track"}`, "repeat track ", "player.repeat"},
		{"/player/transfer", `{"device_id":"d2"}`, "transfer d2 false", "player.transfer"},
		{"/player/queue", `{"uri":"` + plTrack + `"}`, "queue spotify:track:" + plTrack + " ", "player.queue"},
	} {
		h := newPlayerHarness(t)
		code, _ := h.do(t, http.MethodPost, "/admin/api"+tc.path, tc.body)
		if code != http.StatusOK {
			t.Errorf("%s %s: status %d", tc.path, tc.body, code)
			continue
		}
		if len(h.pl.calls) != 1 || h.pl.calls[0] != tc.call {
			t.Errorf("%s: calls %q, want %q", tc.path, h.pl.calls, tc.call)
		}
		if len(h.audit.calls) != 1 || h.audit.calls[0].action != tc.action || h.audit.calls[0].actor != "admin@example.com" {
			t.Errorf("%s: audit %+v", tc.path, h.audit.calls)
		}
	}
}

func TestPlayerCommandsValidate(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/player/volume", `{"volume_percent":101}`},
		{"/player/volume", `{}`},
		{"/player/seek", `{"position_ms":-1}`},
		{"/player/shuffle", `{}`},
		{"/player/repeat", `{"state":"all"}`},
		{"/player/transfer", `{}`},
		{"/player/transfer", `{"device_id":"a b"}`},
		{"/player/queue", `{"uri":"spotify:episode:` + plTrack + `"}`},
		{"/player/play", `{"uris":["nope"]}`},
		{"/player/play", `{"context_uri":"spotify:track:` + plTrack + `"}`},
		{"/player/pause", `{"device_id":"x\ty"}`},
		{"/player/next", `{"unknown":1}`},
		{"/player/next", `{`},
	} {
		h := newPlayerHarness(t)
		code, body := h.do(t, http.MethodPost, "/admin/api"+tc.path, tc.body)
		if code != http.StatusUnprocessableEntity || errCode(t, body) != "validation_failed" {
			t.Errorf("%s %s: %d %s", tc.path, tc.body, code, errCode(t, body))
		}
		if len(h.pl.calls) != 0 || len(h.audit.calls) != 0 {
			t.Errorf("%s %s: calls %q audit %+v", tc.path, tc.body, h.pl.calls, h.audit.calls)
		}
	}
}

func TestPlayerScopeGate(t *testing.T) {
	h := newPlayerHarness(t)
	h.sp.Scopes = []string{"user-read-playback-state"}
	code, body := h.do(t, http.MethodPost, "/admin/api/player/pause", "")
	if code != http.StatusConflict || errCode(t, body) != "missing_scope" || len(h.pl.calls) != 0 {
		t.Errorf("missing scope: %d %s %q", code, errCode(t, body), h.pl.calls)
	}
	// GET still answers, with can_control false.
	code, body = h.do(t, http.MethodGet, "/admin/api/player", "")
	if code != http.StatusOK || string(body["can_control"]) != "false" || string(body["connected"]) != "true" {
		t.Errorf("GET: %d %s", code, body["can_control"])
	}

	h = newPlayerHarness(t)
	h.sp = settings.Spotify{}
	code, body = h.do(t, http.MethodPost, "/admin/api/player/next", "")
	if code != http.StatusConflict || errCode(t, body) != "spotify_not_connected" {
		t.Errorf("not connected: %d %s", code, errCode(t, body))
	}
	h.spErr = settings.ErrNotFound
	code, body = h.do(t, http.MethodGet, "/admin/api/player", "")
	if code != http.StatusOK || string(body["connected"]) != "false" || string(body["state"]) != "null" {
		t.Errorf("GET not connected: %d %v", code, body)
	}

	// A token seeded from the environment has unknown scopes: given a try.
	h = newPlayerHarness(t)
	h.sp.Scopes = nil
	if code, _ := h.do(t, http.MethodPost, "/admin/api/player/next", ""); code != http.StatusOK {
		t.Errorf("unknown scopes: %d", code)
	}
}

func TestPlayerErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		want string
	}{
		{fmt.Errorf("%w: x", spotify.ErrNoActiveDevice), http.StatusConflict, "no_active_device"},
		{fmt.Errorf("%w: x", spotify.ErrPremiumRequired), http.StatusForbidden, "premium_required"},
		{fmt.Errorf("%w: x", spotify.ErrRestricted), http.StatusForbidden, "player_restricted"},
		{fmt.Errorf("%w: x", spotify.ErrNotFound), http.StatusNotFound, "not_found"},
		{&spotify.RateLimitError{RetryAfter: 5e9}, http.StatusTooManyRequests, "rate_limited"},
		{spotify.ErrNotConnected, http.StatusConflict, "spotify_not_connected"},
		{errors.New("boom"), http.StatusBadGateway, "spotify_error"},
	} {
		h := newPlayerHarness(t)
		h.pl.err = tc.err
		code, body := h.do(t, http.MethodPost, "/admin/api/player/next", "")
		if code != tc.code || errCode(t, body) != tc.want {
			t.Errorf("%v: %d %s, want %d %s", tc.err, code, errCode(t, body), tc.code, tc.want)
		}
		if len(h.audit.calls) != 0 {
			t.Errorf("%v: failed command audited", tc.err)
		}
	}
}

func TestGetPlayer(t *testing.T) {
	h := newPlayerHarness(t)
	h.pl.state = &spotify.PlaybackState{IsPlaying: true, RepeatState: "off",
		Item: &spotify.PlayerTrack{ID: plTrack, Name: "Song"}}
	h.pl.devs = []spotify.Device{{ID: "d1", Name: "Desk", Type: "Computer", IsActive: true}}
	h.pl.queue = &spotify.PlayerQueue{Queue: []spotify.PlayerTrack{{ID: plTrack, Name: "Next"}}}
	code, body := h.do(t, http.MethodGet, "/admin/api/player", "")
	if code != http.StatusOK || string(body["can_control"]) != "true" {
		t.Fatalf("%d %v", code, body)
	}
	var v playerView
	b, _ := json.Marshal(body)
	_ = json.Unmarshal(b, &v)
	if v.State == nil || v.State.Item.Name != "Song" || len(v.Devices) != 1 || len(v.Queue) != 1 {
		t.Errorf("view %+v", v)
	}

	// The queue is best effort; nothing loaded hides it.
	h.pl.qErr = spotify.ErrQueueForbidden
	if code, body := h.do(t, http.MethodGet, "/admin/api/player", ""); code != http.StatusOK || string(body["queue"]) != "null" {
		t.Errorf("queue error: %d %s", code, body["queue"])
	}
	h.pl.qErr, h.pl.state = nil, nil
	if code, body := h.do(t, http.MethodGet, "/admin/api/player", ""); code != http.StatusOK ||
		string(body["state"]) != "null" || string(body["queue"]) != "null" {
		t.Errorf("no device: %d %v", code, body)
	}
	h.pl.err = fmt.Errorf("%w: x", spotify.ErrRestricted)
	if code, _ := h.do(t, http.MethodGet, "/admin/api/player", ""); code != http.StatusForbidden {
		t.Errorf("state error: %d", code)
	}
}

func TestSearchPlayer(t *testing.T) {
	h := newPlayerHarness(t)
	code, body := h.do(t, http.MethodGet, "/admin/api/player/search?q=%20love%20&limit=5", "")
	if code != http.StatusOK || len(h.pl.calls) != 1 || h.pl.calls[0] != "search love 5" {
		t.Fatalf("%d %q", code, h.pl.calls)
	}
	if !strings.Contains(string(body["data"]), "Found") {
		t.Errorf("data %s", body["data"])
	}
	for _, q := range []string{"", "?q=", "?q=x&limit=11", "?q=x&limit=0", "?q=" + strings.Repeat("a", 201)} {
		if code, _ := h.do(t, http.MethodGet, "/admin/api/player/search"+q, ""); code != http.StatusUnprocessableEntity {
			t.Errorf("%q: %d", q, code)
		}
	}
	// Search needs a connection but not the control scope.
	h.sp.Scopes = []string{"user-read-playback-state"}
	if code, _ := h.do(t, http.MethodGet, "/admin/api/player/search?q=x", ""); code != http.StatusOK {
		t.Errorf("search without control scope: %d", code)
	}
}

func TestReadOnlyHandlerHasNoPlayerRoutes(t *testing.T) {
	ro := ReadOnlyHandler(&API{player: &fakePlayer{}})
	for _, p := range []string{"/player", "/player/search", "/player/play", "/player/pause", "/player/next",
		"/player/previous", "/player/seek", "/player/volume", "/player/shuffle", "/player/repeat",
		"/player/transfer", "/player/queue"} {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			ro.ServeHTTP(rec, httptest.NewRequest(m, p, nil))
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d", m, p, rec.Code)
			}
		}
	}
}
