package brandkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAPI = "https://baoopn.com/api/v1/brand/listening"

// fakeAPI is an http.RoundTripper that answers with the current body/status
// or error.
type fakeAPI struct {
	mu     sync.Mutex
	status int
	body   []byte
	err    error
	block  bool
	reqs   []*http.Request
}

func (f *fakeAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	status, body, err, block := f.status, f.body, f.err, f.block
	f.mu.Unlock()
	if block {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Request: req,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func (f *fakeAPI) set(status int, body []byte, err error) {
	f.mu.Lock()
	f.status, f.body, f.err = status, body, err
	f.mu.Unlock()
}

// sample returns the saved live response's assets, keyed by slot.
func sample(t *testing.T) map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/listening.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Data struct {
			Assets map[string]map[string]any `json:"assets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Data.Assets
}

func encode(t *testing.T, assets map[string]map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"data": map[string]any{"assets": assets}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestCache(t *testing.T, logs io.Writer) (*Cache, *fakeAPI) {
	t.Helper()
	f := &fakeAPI{status: http.StatusOK}
	if logs == nil {
		logs = io.Discard
	}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c, err := New(testAPI, time.Minute, &http.Client{Transport: f}, log)
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

func TestRefreshGoodFetch(t *testing.T) {
	c, f := newTestCache(t, nil)
	f.set(http.StatusOK, encode(t, sample(t)), nil)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, s := range Slots() {
		a, ok := c.Lookup(s.Name)
		if !ok || !strings.HasPrefix(a.URL, "https://media.baoopn.com/brand/listening/") {
			t.Errorf("%s: %+v, %v", s.Name, a, ok)
		}
	}
	if a, _ := c.Lookup(EmailLogoSlot); a.Width != 640 || a.Height != 264 || a.ContentType != "image/png" {
		t.Errorf("email logo %+v", a)
	}
	req := f.reqs[0]
	if req.URL.String() != testAPI || req.Header.Get("Accept") != "application/json" || req.Header.Get("User-Agent") != userAgent {
		t.Errorf("request %s %v", req.URL, req.Header)
	}
}

func TestRefreshRejectsBadEntries(t *testing.T) {
	var logs bytes.Buffer
	c, f := newTestCache(t, &logs)
	assets := sample(t)
	bad := map[string]map[string]any{
		"mark_primary":          {"url": "https://evil.com/brand/x.svg"},
		"mark_inverse":          {"url": "https://media.baoopn.com.evil.com/x.svg"},
		"mark_navy":             {"url": "http://media.baoopn.com/x.svg"},
		"mark_cream":            {"url": "https://user:pw@media.baoopn.com/x.svg"},
		"wordmark_primary":      {"url": "https://media.baoopn.com:8443/x.svg"},
		"wordmark_inverse":      {"content_type": "image/png"},
		"wordmark_navy":         {"slot": "wordmark_cream"},
		"icon_track_navy":       {"url": "/other/x.svg"},
		"icon_track_cream":      {"url": "//evil.com/x.svg"},
		"icon_paused_navy":      {"url": "https://media.baoopn.com/x\".svg"},
		"icon_paused_cream":     {"url": "https://media.baoopn.com/a b.svg"},
		"icon_playlist_navy":    {"url": ""},
		"icon_headphones_cream": {"content_type": "text/html"},
	}
	for slot, patch := range bad {
		for k, v := range patch {
			assets[slot][k] = v
		}
	}
	// Accepted variations.
	assets["icon_headphones_navy"]["content_type"] = "image/svg+xml; charset=utf-8"
	assets["favicon_ico"]["content_type"] = "image/vnd.microsoft.icon"
	assets["icon_now_playing_navy"]["url"] = "/media/brand/x.svg"
	assets["icon_now_playing_cream"]["url"] = "https://MEDIA.baoopn.com:443/y.svg"
	assets["og_image"]["width"] = -5
	assets["not_a_slot"] = map[string]any{"url": "https://media.baoopn.com/z.svg", "content_type": "image/svg+xml"}

	f.set(http.StatusOK, encode(t, assets), nil)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for slot := range bad {
		if a, ok := c.Lookup(slot); ok {
			t.Errorf("%s accepted: %+v", slot, a)
		}
	}
	if _, ok := c.Lookup("not_a_slot"); ok {
		t.Error("unknown slot cached")
	}
	if a, ok := c.Lookup("icon_headphones_navy"); !ok || a.ContentType != "image/svg+xml; charset=utf-8" {
		t.Errorf("svg with charset: %+v, %v", a, ok)
	}
	if _, ok := c.Lookup("favicon_ico"); !ok {
		t.Error("image/vnd.microsoft.icon rejected")
	}
	if a, _ := c.Lookup("icon_now_playing_navy"); a.URL != "https://baoopn.com/media/brand/x.svg" {
		t.Errorf("relative URL resolved to %q", a.URL)
	}
	if _, ok := c.Lookup("icon_now_playing_cream"); !ok {
		t.Error("upper-case host with :443 rejected")
	}
	if a, _ := c.Lookup("og_image"); a.Width != 0 || a.Height != 630 {
		t.Errorf("negative width kept: %+v", a)
	}
	if n := strings.Count(logs.String(), "brand asset rejected"); n != len(bad) {
		t.Errorf("logged %d rejections, want %d", n, len(bad))
	}
	// The same rejected URLs are not logged again.
	logs.Reset()
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "brand asset rejected") {
		t.Errorf("rejection logged twice:\n%s", logs.String())
	}
}

func TestRefreshFailureKeepsLastGoodCopy(t *testing.T) {
	good := func(t *testing.T) (*Cache, *fakeAPI, *bytes.Buffer) {
		var logs bytes.Buffer
		c, f := newTestCache(t, &logs)
		f.set(http.StatusOK, encode(t, sample(t)), nil)
		if err := c.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		return c, f, &logs
	}
	cases := map[string]func(c *Cache, f *fakeAPI){
		"transport":     func(_ *Cache, f *fakeAPI) { f.set(0, nil, errors.New("connection refused")) },
		"status 500":    func(_ *Cache, f *fakeAPI) { f.set(http.StatusInternalServerError, []byte("oops"), nil) },
		"bad json":      func(_ *Cache, f *fakeAPI) { f.set(http.StatusOK, []byte("{not json"), nil) },
		"no assets":     func(_ *Cache, f *fakeAPI) { f.set(http.StatusOK, []byte(`{"data":{"assets":{}}}`), nil) },
		"all invalid":   func(_ *Cache, f *fakeAPI) { f.set(http.StatusOK, invalidAll(t), nil) },
		"timeout":       func(c *Cache, f *fakeAPI) { c.timeout = 20 * time.Millisecond; f.block = true },
		"not an object": func(_ *Cache, f *fakeAPI) { f.set(http.StatusOK, []byte(`[]`), nil) },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			c, f, logs := good(t)
			before, _ := c.Lookup("mark_primary")
			breakIt(c, f)
			if err := c.Refresh(context.Background()); err == nil {
				t.Fatal("expected an error")
			}
			if a, ok := c.Lookup("mark_primary"); !ok || a != before {
				t.Errorf("snapshot changed: %+v, %v", a, ok)
			}
			if !strings.Contains(logs.String(), "level=WARN msg=\"brand API refresh failed; keeping last good copy\"") ||
				!strings.Contains(logs.String(), "cached=true") {
				t.Errorf("failure not logged as a warning:\n%s", logs)
			}
			logs.Reset()
			_ = c.Refresh(context.Background())
			if !strings.Contains(logs.String(), "level=DEBUG") || strings.Contains(logs.String(), "level=WARN") {
				t.Errorf("second failure should log at debug:\n%s", logs)
			}
		})
	}
}

func invalidAll(t *testing.T) []byte {
	assets := sample(t)
	for _, a := range assets {
		a["url"] = "https://evil.com/x"
	}
	return encode(t, assets)
}

func TestColdStartDown(t *testing.T) {
	var logs bytes.Buffer
	c, f := newTestCache(t, &logs)
	f.set(0, nil, errors.New("no route to host"))
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if _, ok := c.Lookup("mark_primary"); ok {
		t.Error("cold cache returned an asset")
	}
	if !strings.Contains(logs.String(), "cached=false") {
		t.Errorf("cold failure log: %s", logs.String())
	}
	f.set(http.StatusOK, encode(t, sample(t)), nil)
	logs.Reset()
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "brand assets refreshed") || !strings.Contains(logs.String(), "assets=31") {
		t.Errorf("recovery not logged: %s", logs.String())
	}
}

func TestRefreshMergesPerSlot(t *testing.T) {
	c, f := newTestCache(t, nil)
	f.set(http.StatusOK, encode(t, sample(t)), nil)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	old, _ := c.Lookup("mark_navy")
	assets := sample(t)
	assets["mark_primary"]["url"] = "https://media.baoopn.com/brand/listening/mark-primary.new.svg"
	assets["mark_navy"]["url"] = "https://evil.com/x.svg"
	delete(assets, "og_image")
	f.set(http.StatusOK, encode(t, assets), nil)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, _ := c.Lookup("mark_primary"); !strings.HasSuffix(a.URL, ".new.svg") {
		t.Errorf("mark_primary not updated: %s", a.URL)
	}
	if a, _ := c.Lookup("mark_navy"); a != old {
		t.Errorf("invalid update replaced mark_navy: %+v", a)
	}
	if _, ok := c.Lookup("og_image"); !ok {
		t.Error("missing slot dropped the previous copy")
	}
}

func TestDisabledAndInvalidURL(t *testing.T) {
	c, err := New("  ", 0, nil, nil)
	if err != nil || c == nil || c.Enabled() {
		t.Fatalf("empty URL: %v, %v", c, err)
	}
	if c.every != defaultEvery || c.client.Timeout != defaultTimeout {
		t.Errorf("defaults: every=%s timeout=%s", c.every, c.client.Timeout)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Errorf("disabled Run = %v", err)
	}
	if err := c.Refresh(context.Background()); err == nil {
		t.Error("disabled Refresh should fail")
	}
	if _, ok := c.Lookup("mark_primary"); ok {
		t.Error("disabled cache returned an asset")
	}
	for _, bad := range []string{"ftp://x", "http://baoopn.com/api", "https://u:p@baoopn.com/api", "/relative", "https://"} {
		c, err := New(bad, 0, nil, nil)
		if err == nil || c == nil || c.Enabled() {
			t.Errorf("New(%q) = %v, %v; want disabled + error", bad, c, err)
		}
	}
	var nilCache *Cache
	if nilCache.Enabled() {
		t.Error("nil cache enabled")
	}
	if _, ok := nilCache.Lookup("mark_primary"); ok {
		t.Error("nil cache lookup")
	}
}

func TestRunRefreshesAndStops(t *testing.T) {
	c, f := newTestCache(t, nil)
	c.every = 10 * time.Millisecond
	f.set(http.StatusOK, encode(t, sample(t)), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		n := len(f.reqs)
		f.mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d refreshes", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	if _, ok := c.Lookup("mark_primary"); !ok {
		t.Error("Run did not fill the cache")
	}
}

func TestEmailLogoAdapter(t *testing.T) {
	c, f := newTestCache(t, nil)
	logo := EmailLogo(c)
	if _, _, _, ok := logo(); ok {
		t.Error("cold cache returned a logo")
	}
	f.set(http.StatusOK, encode(t, sample(t)), nil)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, w, h, ok := logo()
	if !ok || !strings.HasPrefix(u, "https://media.baoopn.com/") || w != 640 || h != 264 {
		t.Errorf("logo = %q %d %d %v", u, w, h, ok)
	}
	if _, _, _, ok := EmailLogo(nil)(); ok {
		t.Error("nil cache returned a logo")
	}
}
