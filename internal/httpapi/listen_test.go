package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/listen"
)

type fakeLocator struct {
	url       *listen.AudioURL
	err       error
	trackID   string
	remaining time.Duration
	calls     int
}

func (f *fakeLocator) Locate(_ context.Context, id string, remaining time.Duration) (*listen.AudioURL, error) {
	f.calls++
	f.trackID, f.remaining = id, remaining
	return f.url, f.err
}

// allowAll stands in for the listener middleware.
func allowAll(next http.Handler) http.Handler { return next }

func me(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"email": "guest@example.com"}})
}

func listenRouter(o *ListenOptions, snap []byte) http.Handler {
	h := &Handler{log: slog.New(slog.DiscardHandler), opts: Options{Listen: o},
		playback: func(context.Context) ([]byte, error) { return snap, nil }, now: func() time.Time { return lyricsNow }}
	r := chi.NewRouter()
	r.Route("/api/v1", h.mountListen)
	return r
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET %s: bad body %q", path, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("GET %s: Cache-Control = %q", path, cc)
	}
	return rec, out
}

func TestListenConfig(t *testing.T) {
	for _, c := range []struct {
		o    *ListenOptions
		want string
	}{
		{&ListenOptions{Enabled: true, ClerkPublishableKey: "pk_test_x"}, `{"enabled":true,"clerk_publishable_key":"pk_test_x"}`},
		{&ListenOptions{Enabled: true}, `{"enabled":true,"clerk_publishable_key":null}`},
		{&ListenOptions{Enabled: false, ClerkPublishableKey: "pk_test_x"}, `{"enabled":false,"clerk_publishable_key":null}`},
	} {
		rec, out := get(t, listenRouter(c.o, nil), "/api/v1/listen/config")
		if rec.Code != 200 || string(out["data"]) != c.want {
			t.Errorf("config = %d %s, want %s", rec.Code, out["data"], c.want)
		}
	}
}

func TestListenDisabledAndUnavailable(t *testing.T) {
	for _, path := range []string{"/api/v1/listen/me", "/api/v1/listen/audio"} {
		rec, out := get(t, listenRouter(&ListenOptions{Enabled: false, Auth: allowAll, Me: me}, nil), path)
		if rec.Code != 404 || !strings.Contains(string(out["error"]), `"listen_disabled"`) {
			t.Errorf("disabled %s = %d %s", path, rec.Code, out)
		}
		rec, out = get(t, listenRouter(&ListenOptions{Enabled: true}, nil), path)
		if rec.Code != 503 || !strings.Contains(string(out["error"]), `"listen_unavailable"`) {
			t.Errorf("no auth %s = %d %s", path, rec.Code, out)
		}
	}
	// Auth refusals pass through.
	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in required")
		})
	}
	rec, _ := get(t, listenRouter(&ListenOptions{Enabled: true, Auth: deny, Me: me}, nil), "/api/v1/listen/audio")
	if rec.Code != 401 {
		t.Errorf("denied = %d", rec.Code)
	}
	rec, out := get(t, listenRouter(&ListenOptions{Enabled: true, Auth: allowAll, Me: me}, nil), "/api/v1/listen/me")
	if rec.Code != 200 || !strings.Contains(string(out["data"]), "guest@example.com") {
		t.Errorf("me = %d %s", rec.Code, out)
	}
}

const trackID = "4uLU6hMCjMI75M1A2tKUQC"

func TestListenAudioReasons(t *testing.T) {
	fresh := lyricsNow.Add(-2 * time.Second)
	playing := snapshot(true, ms(60_000), fresh, trackID, 200_000)
	exp := lyricsNow.Add(10 * time.Minute)
	found := &fakeLocator{url: &listen.AudioURL{URL: "https://s3.example.com/a.mp3?sig", ExpiresAt: exp, ContentType: "audio/mpeg"}}
	cases := []struct {
		name string
		snap []byte
		loc  AudioLocator
		want string
	}{
		{"null snapshot", []byte("null"), found, `{"track_id":null,"url":null,"expires_at":null,"content_type":null,"reason":"idle"}`},
		{"stale", snapshot(true, ms(1000), lyricsNow.Add(-2*time.Minute), trackID, 200_000), found,
			`{"track_id":null,"url":null,"expires_at":null,"content_type":null,"reason":"idle"}`},
		{"past end", snapshot(true, ms(199_000), fresh, trackID, 200_000), found,
			`{"track_id":null,"url":null,"expires_at":null,"content_type":null,"reason":"idle"}`},
		{"no progress", snapshot(true, nil, fresh, trackID, 200_000), found,
			`{"track_id":null,"url":null,"expires_at":null,"content_type":null,"reason":"idle"}`},
		{"storage unconfigured", playing, nil,
			`{"track_id":"` + trackID + `","url":null,"expires_at":null,"content_type":null,"reason":"storage_unconfigured"}`},
		{"not found", playing, &fakeLocator{},
			`{"track_id":"` + trackID + `","url":null,"expires_at":null,"content_type":null,"reason":"not_found"}`},
		{"unavailable", playing, &fakeLocator{err: errors.New("storage answered 500")},
			`{"track_id":"` + trackID + `","url":null,"expires_at":null,"content_type":null,"reason":"unavailable"}`},
		{"found", playing, found, `{"track_id":"` + trackID + `","url":"https://s3.example.com/a.mp3?sig","expires_at":"2026-11-24T18:10:00Z","content_type":"audio/mpeg","reason":null}`},
		{"paused still gets a url", snapshot(false, ms(60_000), fresh, trackID, 200_000), found,
			`{"track_id":"` + trackID + `","url":"https://s3.example.com/a.mp3?sig","expires_at":"2026-11-24T18:10:00Z","content_type":"audio/mpeg","reason":null}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, out := get(t, listenRouter(&ListenOptions{Enabled: true, Auth: allowAll, Me: me, Audio: c.loc}, c.snap), "/api/v1/listen/audio")
			if rec.Code != 200 || string(out["data"]) != c.want {
				t.Errorf("got %d %s\nwant %s", rec.Code, out["data"], c.want)
			}
		})
	}
}

func TestListenAudioRemaining(t *testing.T) {
	fresh := lyricsNow.Add(-2 * time.Second)
	loc := &fakeLocator{}
	o := &ListenOptions{Enabled: true, Auth: allowAll, Me: me, Audio: loc}
	get(t, listenRouter(o, snapshot(true, ms(60_000), fresh, trackID, 200_000)), "/api/v1/listen/audio")
	// 60 s + 2 s since the snapshot: 138 s left.
	if loc.trackID != trackID || loc.remaining != 138*time.Second {
		t.Errorf("locate(%q, %v)", loc.trackID, loc.remaining)
	}
	get(t, listenRouter(o, snapshot(false, ms(60_000), fresh, trackID, 200_000)), "/api/v1/listen/audio")
	if loc.remaining != 140*time.Second {
		t.Errorf("paused remaining = %v", loc.remaining)
	}
	get(t, listenRouter(o, snapshot(true, ms(60_000), fresh, trackID, 0)), "/api/v1/listen/audio")
	if loc.remaining != 0 {
		t.Errorf("unknown duration remaining = %v", loc.remaining)
	}
}

// The MCP server's in-process handler never serves listen along.
func TestV1HandlerHasNoListenRoutes(t *testing.T) {
	h := &Handler{log: slog.New(slog.DiscardHandler), opts: Options{Listen: &ListenOptions{Enabled: true, Auth: allowAll, Me: me}}}
	for _, p := range []string{"/api/v1/listen/config", "/api/v1/listen/me", "/api/v1/listen/audio",
		"/api/v1/listen/progress", "/api/v1/listen/history", "/api/v1/listen/presence"} {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			h.V1Handler().ServeHTTP(rec, httptest.NewRequest(m, p, nil))
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("V1Handler %s %s = %d", m, p, rec.Code)
			}
		}
	}
}

func TestListenNotMountedWithoutOptions(t *testing.T) {
	r := NewRouter(nil, nil, slog.New(slog.DiscardHandler), Options{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/listen/config", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("config without Listen = %d", rec.Code)
	}
	r = NewRouter(nil, nil, slog.New(slog.DiscardHandler), Options{Listen: &ListenOptions{Enabled: true}})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/listen/config", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("config with Listen = %d %v", rec.Code, rec.Header())
	}
}
