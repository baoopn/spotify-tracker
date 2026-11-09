package spotify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder logs "METHOD path?query" of every request.
type recorder struct {
	mu   sync.Mutex
	reqs []string
}

func (r *recorder) add(req *http.Request) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := req.Method + " " + req.URL.Path
	if req.URL.RawQuery != "" {
		s += "?" + req.URL.Query().Encode()
	}
	r.reqs = append(r.reqs, s)
	return s
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reqs...)
}

const (
	libraryReq   = "/v1/me/library?uris=spotify%3Aplaylist%3Apl1"
	followersReq = "/v1/playlists/pl1/followers"
)

func TestUnfollowPlaylist(t *testing.T) {
	rec := &recorder{}
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if raw, _ := io.ReadAll(r.Body); len(raw) != 0 || r.Header.Get("Content-Type") != "" {
			t.Errorf("unexpected body %q", raw)
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := m.UnfollowPlaylist(context.Background(), "pl1"); err != nil {
		t.Fatal(err)
	}
	if got := rec.list(); len(got) != 1 || got[0] != "DELETE "+libraryReq {
		t.Fatalf("requests %v", got)
	}
}

func TestUnfollowPlaylistNotFoundIsNil(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if err := m.UnfollowPlaylist(context.Background(), "pl1"); err != nil {
		t.Fatalf("404 should be nil, got %v", err)
	}
}

func TestUnfollowPlaylistFallsBackOn403(t *testing.T) {
	rec := &recorder{}
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		if rec.add(r) == "DELETE "+libraryReq {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := m.UnfollowPlaylist(context.Background(), "pl1"); err != nil {
		t.Fatal(err)
	}
	if got := rec.list(); len(got) != 2 || got[1] != "DELETE "+followersReq {
		t.Fatalf("requests %v", got)
	}
}

func TestFollowPlaylist(t *testing.T) {
	rec := &recorder{}
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.WriteHeader(http.StatusOK)
	})
	if err := m.FollowPlaylist(context.Background(), "pl1"); err != nil {
		t.Fatal(err)
	}
	if got := rec.list(); len(got) != 1 || got[0] != "PUT "+libraryReq {
		t.Fatalf("requests %v", got)
	}
}

func TestFollowPlaylistFallback(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			rec := &recorder{}
			m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
				if rec.add(r) == "PUT "+libraryReq {
					w.WriteHeader(status)
					return
				}
				if raw, _ := io.ReadAll(r.Body); string(raw) != `{"public":true}` {
					t.Errorf("bad body %s", raw)
				}
				w.WriteHeader(http.StatusOK)
			})
			if err := m.FollowPlaylist(context.Background(), "pl1"); err != nil {
				t.Fatal(err)
			}
			if got := rec.list(); len(got) != 2 || got[1] != "PUT "+followersReq {
				t.Fatalf("requests %v", got)
			}
		})
	}
}

func TestFollowPlaylistNotFound(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if err := m.FollowPlaylist(context.Background(), "pl1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFollowUnfollowRefreshAndRateLimit(t *testing.T) {
	var calls atomic.Int32
	m, forced := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := m.FollowPlaylist(context.Background(), "pl1"); err != nil || forced.Load() != 1 || calls.Load() != 2 {
		t.Fatalf("follow 401 retry: err=%v forced=%d calls=%d", err, forced.Load(), calls.Load())
	}
	if err := m.UnfollowPlaylist(context.Background(), "pl1"); err != nil || forced.Load() != 2 || calls.Load() != 4 {
		t.Fatalf("unfollow 401 retry: err=%v forced=%d calls=%d", err, forced.Load(), calls.Load())
	}

	m, _ = writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "9")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var rl *RateLimitError
	if err := m.FollowPlaylist(context.Background(), "pl1"); !errors.As(err, &rl) || rl.RetryAfter != 9*time.Second {
		t.Fatalf("follow: want RateLimitError(9s), got %v", err)
	}
	if err := m.UnfollowPlaylist(context.Background(), "pl1"); !errors.As(err, &rl) {
		t.Fatalf("unfollow: want RateLimitError, got %v", err)
	}
}

func TestOwnedPlaylistIDsPaginates(t *testing.T) {
	rec := &recorder{}
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if r.URL.Path != "/v1/me/playlists" || r.URL.Query().Get("limit") != "50" {
			t.Errorf("got %s", r.URL)
		}
		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = fmt.Fprint(w, `{"items":[{"id":"a"},null,{"id":"b"}],"next":"https://api.spotify.com/v1/me/playlists?offset=50&limit=50","total":53}`)
		case "50":
			_, _ = fmt.Fprint(w, `{"items":[{"id":"c"}],"next":null,"total":53}`)
		default:
			t.Errorf("unexpected offset %s", r.URL.Query().Get("offset"))
		}
	})
	ids, err := m.OwnedPlaylistIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || !ids["a"] || !ids["b"] || !ids["c"] {
		t.Fatalf("ids %v", ids)
	}
	if n := len(rec.list()); n != 2 {
		t.Fatalf("%d requests, want 2", n)
	}
}

func TestOwnedPlaylistIDsErrors(t *testing.T) {
	var calls atomic.Int32
	m, forced := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, `{"items":[{"id":"a"}],"next":null}`)
	})
	if ids, err := m.OwnedPlaylistIDs(context.Background()); err != nil || !ids["a"] || forced.Load() != 1 {
		t.Fatalf("401 retry: ids=%v err=%v forced=%d", ids, err, forced.Load())
	}

	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		m, _ = writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		if ids, err := m.OwnedPlaylistIDs(context.Background()); err == nil || ids != nil {
			t.Fatalf("%d: want error, got %v", status, ids)
		}
	}

	m, _ = writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var rl *RateLimitError
	if _, err := m.OwnedPlaylistIDs(context.Background()); !errors.As(err, &rl) {
		t.Fatalf("want RateLimitError, got %v", err)
	}

	// A library longer than the page cap is an error, never a partial list.
	m, _ = writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"items":[{"id":"x%s"}],"next":"more"}`, r.URL.Query().Get("offset"))
	})
	if ids, err := m.OwnedPlaylistIDs(context.Background()); err == nil || ids != nil {
		t.Fatalf("want error for a truncated list, got %d ids", len(ids))
	}
}

func TestSendJSONNotFound(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if err := m.ReplacePlaylistItems(context.Background(), "gone", []string{"spotify:track:a"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestCurrentUserName(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"id":"u1","display_name":"alex"}`, "alex"},
		{`{"id":"u1","display_name":""}`, "u1"},
	} {
		m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v1/me" {
				t.Errorf("got %s %s", r.Method, r.URL.Path)
			}
			_, _ = fmt.Fprint(w, tc.body)
		})
		if got, err := m.CurrentUserName(context.Background()); err != nil || got != tc.want {
			t.Fatalf("got %q %v, want %q", got, err, tc.want)
		}
	}

	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{}`) })
	if _, err := m.CurrentUserName(context.Background()); err == nil {
		t.Fatal("want error for an empty profile")
	}
	m, _ = writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	if _, err := m.CurrentUserName(context.Background()); err == nil {
		t.Fatal("want error on 403")
	}
	var calls atomic.Int32
	m, forced := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"u1","display_name":"alex"}`)
	})
	if got, err := m.CurrentUserName(context.Background()); err != nil || got != "alex" || forced.Load() != 1 {
		t.Fatalf("401 retry: %q %v forced=%d", got, err, forced.Load())
	}
	m, _ = writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var rl *RateLimitError
	if _, err := m.CurrentUserName(context.Background()); !errors.As(err, &rl) {
		t.Fatalf("want RateLimitError, got %v", err)
	}
}
