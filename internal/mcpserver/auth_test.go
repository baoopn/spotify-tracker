package mcpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/settings"
)

type fakeSettings struct {
	mu       sync.Mutex
	cur      settings.MCP
	err      error
	loads    int
	touches  []time.Time
	touchErr error
}

func (f *fakeSettings) MCP(context.Context) (settings.MCP, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	return f.cur, f.err
}

func (f *fakeSettings) TouchMCPToken(_ context.Context, hash string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if hash == f.cur.TokenHash {
		f.touches = append(f.touches, at)
	}
	return f.touchErr
}

const storedToken = "stm_stored-token-for-tests-0123456789abcdefghijk"

func newCacheServer(t *testing.T, f *fakeSettings, envToken string) (*SettingsCache, *httptest.Server, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	c := NewSettingsCache(f, time.Minute, slog.New(slog.DiscardHandler))
	c.now = func() time.Time { return now }
	srv := httptest.NewServer(New(Options{Public: fakePublic(), Admin: fakeAdmin(), AdminToken: envToken,
		Settings: c, Log: slog.New(slog.DiscardHandler)}))
	t.Cleanup(srv.Close)
	return c, srv, &now
}

func listStatus(t *testing.T, srv *httptest.Server, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestStoredTokenAndTouchThrottle(t *testing.T) {
	f := &fakeSettings{cur: settings.MCP{AdminToolsEnabled: true, TokenHash: settings.HashMCPToken(storedToken)}}
	c, srv, now := newCacheServer(t, f, "")

	for range 3 {
		code, body := listStatus(t, srv, storedToken)
		if code != http.StatusOK || !strings.Contains(body, "admin_overview") {
			t.Fatalf("stored token = %d %s", code, body)
		}
	}
	c.wg.Wait()
	if len(f.touches) != 1 || f.loads != 1 {
		t.Fatalf("touches %d, loads %d; want 1 and 1 (cached)", len(f.touches), f.loads)
	}

	// After the throttle window (and the cache TTL) the next use is recorded.
	*now = now.Add(touchEvery + time.Second)
	f.touchErr = errors.New("db down") // best effort: never fails the call
	if code, _ := listStatus(t, srv, storedToken); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	c.wg.Wait()
	if len(f.touches) != 2 || f.loads != 2 {
		t.Errorf("touches %d, loads %d; want 2 and 2", len(f.touches), f.loads)
	}
}

func TestSettingsUnavailable(t *testing.T) {
	f := &fakeSettings{err: errors.New("postgres down")}
	_, srv, _ := newCacheServer(t, f, testToken)

	// The env token still works (admin tools default to on).
	if code, body := listStatus(t, srv, testToken); code != http.StatusOK || !strings.Contains(body, "admin_overview") {
		t.Errorf("env token = %d %s", code, body)
	}
	// Any other token cannot be checked: 503, not a misleading 401.
	if code, _ := listStatus(t, srv, storedToken); code != http.StatusServiceUnavailable {
		t.Errorf("stored token = %d, want 503", code)
	}
}

func TestCacheInvalidateAndStale(t *testing.T) {
	f := &fakeSettings{cur: settings.DefaultMCP}
	c := NewSettingsCache(f, time.Hour, nil)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	ctx := context.Background()
	if v, err := c.get(ctx); err != nil || !v.AdminToolsEnabled {
		t.Fatalf("get = %+v, %v", v, err)
	}
	f.cur = settings.MCP{AdminToolsEnabled: false}
	if v, _ := c.get(ctx); !v.AdminToolsEnabled {
		t.Error("cache not used within the TTL")
	}
	c.Invalidate()
	if v, _ := c.get(ctx); v.AdminToolsEnabled {
		t.Error("Invalidate did not reload")
	}
	// A failing reload after the TTL keeps the last value.
	now = now.Add(2 * time.Hour)
	f.err = errors.New("down")
	if v, err := c.get(ctx); err != nil || v.AdminToolsEnabled {
		t.Errorf("stale get = %+v, %v", v, err)
	}
	// Nothing stored yet means the defaults.
	f.err = settings.ErrNotFound
	c2 := NewSettingsCache(f, time.Hour, nil)
	if v, err := c2.get(ctx); err != nil || !v.AdminToolsEnabled || v.TokenHash != "" {
		t.Errorf("not found = %+v, %v", v, err)
	}
}

// TestCacheInvalidateThenFail: after Invalidate (save or revoke) a failing
// reload makes the settings unavailable instead of serving the old value.
func TestCacheInvalidateThenFail(t *testing.T) {
	f := &fakeSettings{cur: settings.MCP{AdminToolsEnabled: true, TokenHash: settings.HashMCPToken(storedToken)}}
	c, srv, _ := newCacheServer(t, f, testToken)
	if code, _ := listStatus(t, srv, storedToken); code != http.StatusOK {
		t.Fatalf("stored token = %d", code)
	}
	f.mu.Lock()
	f.err = errors.New("postgres down")
	f.mu.Unlock()
	c.Invalidate()
	if code, _ := listStatus(t, srv, storedToken); code != http.StatusServiceUnavailable {
		t.Errorf("stored token after invalidate + failed reload = %d, want 503", code)
	}
	// The env token still works.
	if code, _ := listStatus(t, srv, testToken); code != http.StatusOK {
		t.Errorf("env token = %d", code)
	}
	c.wg.Wait()
}

// TestCacheRetryBackoff: after a failed load the cache does not query the
// source again until loadRetry has passed.
func TestCacheRetryBackoff(t *testing.T) {
	f := &fakeSettings{err: errors.New("postgres down")}
	c := NewSettingsCache(f, time.Minute, nil)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	ctx := context.Background()
	for range 5 {
		if _, err := c.get(ctx); err == nil {
			t.Fatal("get succeeded with the source down")
		}
	}
	if f.loads != 1 {
		t.Errorf("loads during backoff = %d, want 1", f.loads)
	}
	now = now.Add(loadRetry)
	f.mu.Lock()
	f.err, f.cur = nil, settings.DefaultMCP
	f.mu.Unlock()
	if v, err := c.get(ctx); err != nil || !v.AdminToolsEnabled || f.loads != 2 {
		t.Errorf("after backoff = %+v, %v, loads %d", v, err, f.loads)
	}
}

// TestCacheConcurrentLoadsShared: concurrent cold gets share one load.
func TestCacheConcurrentLoadsShared(t *testing.T) {
	release := make(chan struct{})
	f := &blockingSettings{release: release}
	c := NewSettingsCache(f, time.Minute, nil)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := c.get(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := f.loads.Load(); n != 1 {
		t.Errorf("loads = %d, want 1", n)
	}
}

type blockingSettings struct {
	release chan struct{}
	loads   atomic.Int32
}

func (b *blockingSettings) MCP(context.Context) (settings.MCP, error) {
	b.loads.Add(1)
	<-b.release
	return settings.DefaultMCP, nil
}

func (b *blockingSettings) TouchMCPToken(context.Context, string, time.Time) error { return nil }

func TestToolInfos(t *testing.T) {
	if len(PublicTools()) != len(publicTools) || len(AdminTools()) != len(adminTools) {
		t.Fatal("tool info length mismatch")
	}
	for _, ti := range append(PublicTools(), AdminTools()...) {
		if ti.Name == "" || ti.Description == "" {
			t.Errorf("incomplete tool info %+v", ti)
		}
	}
}
