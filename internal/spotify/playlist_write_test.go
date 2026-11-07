package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// writeManager returns a Manager talking to handler, with tokens "at"
// (cached) and "fresh" (forced refresh). forced counts forced refreshes.
func writeManager(t *testing.T, handler http.HandlerFunc) (*Manager, *atomic.Int32) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	m := NewManager(&OAuth{APIBase: srv.URL + "/v1"}, nil, nil, slog.New(slog.DiscardHandler))
	var forced atomic.Int32
	m.tokens = func(_ context.Context, force bool) (string, time.Time, error) {
		if force {
			forced.Add(1)
			return "fresh", time.Now().Add(time.Hour), nil
		}
		return "at", time.Now().Add(time.Hour), nil
	}
	return m, &forced
}

func TestCreatePlaylist(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/me/playlists" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "Bearer at" {
			t.Errorf("bad headers %v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "Top 50 · 2026" || body["public"] != true || body["description"] != "desc" {
			t.Errorf("bad body %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"pl1","external_urls":{"spotify":"https://open.spotify.com/playlist/pl1"}}`))
	})
	id, link, err := m.CreatePlaylist(context.Background(), "Top 50 · 2026", "desc", true)
	if err != nil || id != "pl1" || link != "https://open.spotify.com/playlist/pl1" {
		t.Fatalf("got %q %q %v", id, link, err)
	}
}

func TestCreatePlaylistDefaultsURL(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"pl2"}`))
	})
	_, link, err := m.CreatePlaylist(context.Background(), "n", "d", true)
	if err != nil || link != "https://open.spotify.com/playlist/pl2" {
		t.Fatalf("got %q %v", link, err)
	}
}

func TestReplacePlaylistItems(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/playlists/pl1/items" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != `{"uris":["spotify:track:a","spotify:track:b"]}` {
			t.Errorf("bad body %s", raw)
		}
		_, _ = w.Write([]byte(`{"snapshot_id":"s1"}`))
	})
	if err := m.ReplacePlaylistItems(context.Background(), "pl1", []string{"spotify:track:a", "spotify:track:b"}); err != nil {
		t.Fatal(err)
	}
}

func TestReplacePlaylistItemsTooMany(t *testing.T) {
	var calls atomic.Int32
	m, _ := writeManager(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	uris := make([]string, MaxPlaylistItems+1)
	for i := range uris {
		uris[i] = "spotify:track:x"
	}
	if err := m.ReplacePlaylistItems(context.Background(), "pl1", uris); err == nil {
		t.Fatal("want error for more than 100 uris")
	}
	if calls.Load() != 0 {
		t.Error("no request expected")
	}
}

func TestSendJSONRefreshesOn401(t *testing.T) {
	var calls atomic.Int32
	m, forced := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"uris"`) {
			t.Errorf("body not resent on retry: %s", raw)
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"snapshot_id":"s1"}`))
	})
	if err := m.ReplacePlaylistItems(context.Background(), "pl1", []string{"spotify:track:a"}); err != nil {
		t.Fatal(err)
	}
	if forced.Load() != 1 || calls.Load() != 2 {
		t.Errorf("forced=%d calls=%d, want 1 and 2", forced.Load(), calls.Load())
	}
}

func TestSendJSONErrors(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"status":403,"message":"Insufficient client scope"}}`))
	})
	if _, _, err := m.CreatePlaylist(context.Background(), "n", "d", true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}

	m, _ = writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var rl *RateLimitError
	if err := m.ReplacePlaylistItems(context.Background(), "p", nil); !errors.As(err, &rl) || rl.RetryAfter != 42*time.Second {
		t.Fatalf("want RateLimitError(42s), got %v", err)
	}

	m, _ = writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if err := m.ReplacePlaylistItems(context.Background(), "p", nil); err == nil {
		t.Fatal("want error after repeated 401")
	}
}

func TestScopesIncludePlaylistWrite(t *testing.T) {
	if !HasScope(Scopes, PlaylistWriteScope) {
		t.Errorf("Scopes %v lack %s", Scopes, PlaylistWriteScope)
	}
}
