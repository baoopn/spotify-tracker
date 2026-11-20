package mcpserver_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/admin"
	"github.com/baoopn/spotify-tracker/internal/httpapi"
	"github.com/baoopn/spotify-tracker/internal/mcpserver"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

const e2eToken = "e2e-admin-token-0123456789abcdef0123"

// staticSettings is an in-memory settings.Store stand-in.
type staticSettings struct{ v settings.MCP }

func (s staticSettings) MCP(context.Context) (settings.MCP, error) { return s.v, nil }
func (staticSettings) TouchMCPToken(context.Context, string, time.Time) error {
	return nil
}

// deadRedis returns a client whose server closes every connection, so the
// cache and now-playing lookups fail fast.
func deadRedis(t *testing.T) *redis.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	rdb := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), MaxRetries: -1,
		DialTimeout: time.Second, ReadTimeout: time.Second})
	t.Cleanup(func() { _ = rdb.Close(); _ = ln.Close() })
	return rdb
}

type e2e struct {
	srv       *httptest.Server
	v1Counted *atomic.Int64 // requests seen by the public analytics middleware
}

// newE2E wires /mcp like cmd/api: the real router, the real v1 handler (no
// store: data routes answer 500) and the real admin ReadOnlyHandler.
func newE2E(t *testing.T, mcpLimit, v1Limit int) *e2e {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	var counted atomic.Int64
	cache := mcpserver.NewSettingsCache(staticSettings{v: settings.DefaultMCP}, time.Minute, log)
	h := httpapi.NewRouter(nil, deadRedis(t), log, httpapi.Options{
		RateLimitPerMinute: v1Limit,
		V1Middleware: []func(http.Handler) http.Handler{func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				counted.Add(1)
				next.ServeHTTP(w, r)
			})
		}},
		Events: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
		MCP: func(v1 http.Handler) http.Handler {
			return mcpserver.New(mcpserver.Options{Public: v1, Admin: admin.ReadOnlyHandler(&admin.API{}),
				AdminToken: e2eToken, Settings: cache, Log: log})
		},
		MCPRateLimitPerMinute: mcpLimit,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &e2e{srv: srv, v1Counted: &counted}
}

type authTransport struct{ auth string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if a.auth != "" {
		r.Header.Set("Authorization", a.auth)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (e *e2e) connect(t *testing.T, auth string) (*mcp.ClientSession, error) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             e.srv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: authTransport{auth: auth}, Timeout: 10 * time.Second},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err == nil {
		t.Cleanup(func() { _ = cs.Close() })
	}
	return cs, err
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func toolCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// TestE2EThroughRouter runs a real MCP client against the full router.
func TestE2EThroughRouter(t *testing.T) {
	e := newE2E(t, 1000, 1000)

	cs, err := e.connect(t, "")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != len(mcpserver.PublicTools()) {
		t.Errorf("anonymous: %d tools, want %d", len(tools.Tools), len(mcpserver.PublicTools()))
	}

	// 4xx from the real handler: its message becomes the tool error.
	res := toolCall(t, cs, "recent_plays", map[string]any{"cursor": "yesterday"})
	if !res.IsError || text(res) != "cursor must be an RFC 3339 timestamp" {
		t.Errorf("bad cursor = %v %q", res.IsError, text(res))
	}
	// 5xx (no store: the handler panics, the v1 recoverer answers 500):
	// a generic tool error, no internals.
	for _, name := range []string{"recent_plays", "now_playing", "top_tracks", "track_details"} {
		res = toolCall(t, cs, name, map[string]any{"id": "4uLU6hMCjMI75M1A2tKUQC"})
		if !res.IsError || text(res) != "internal error" {
			t.Errorf("%s = %v %q, want internal error", name, res.IsError, text(res))
		}
	}
	// Admin tools are not callable anonymously.
	if res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "admin_history"}); err == nil && !res.IsError {
		t.Errorf("admin_history anonymously = %s", text(res))
	}
	// In-process tool calls bypass the public analytics middleware.
	if n := e.v1Counted.Load(); n != 0 {
		t.Errorf("MCP calls counted %d times in the public API analytics", n)
	}

	acs, err := e.connect(t, "Bearer "+e2eToken)
	if err != nil {
		t.Fatal(err)
	}
	tools, err = acs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(mcpserver.PublicTools()) + len(mcpserver.AdminTools()); len(tools.Tools) != want {
		t.Errorf("admin: %d tools, want %d", len(tools.Tools), want)
	}
	res = toolCall(t, acs, "admin_history", map[string]any{"cursor": "nope"})
	if !res.IsError || text(res) != "cursor must be an RFC 3339 timestamp" {
		t.Errorf("admin bad cursor = %v %q", res.IsError, text(res))
	}

	if _, err := e.connect(t, "Bearer wrong-token"); err == nil {
		t.Error("wrong token connected")
	}
}

func post(t *testing.T, url, body string) int {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

const listBody = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

// TestE2ERateLimit: /mcp has its own per-IP limit, separate from /api/v1.
func TestE2ERateLimit(t *testing.T) {
	e := newE2E(t, 3, 2)
	for i := range 3 {
		if code := post(t, e.srv.URL+"/mcp", listBody); code != http.StatusOK {
			t.Fatalf("request %d = %d", i+1, code)
		}
	}
	code := post(t, e.srv.URL+"/mcp", listBody)
	if code != http.StatusTooManyRequests {
		t.Errorf("4th request = %d, want 429", code)
	}
	// Wrong tokens count too (no free brute force).
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, e.srv.URL+"/mcp", strings.NewReader(listBody))
	req.Header.Set("Authorization", "Bearer guess")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = r2.Body.Close()
	if r2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("wrong token over the limit = %d, want 429", r2.StatusCode)
	}

	// The public API budget is untouched by MCP traffic.
	for i := range 2 {
		r, err := http.Get(e.srv.URL + "/api/v1/timezone?tz=UTC") //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("v1 request %d = %d", i+1, r.StatusCode)
		}
	}
}

// TestE2EHTTPEdges: methods, body cap and CORS of the /mcp route.
func TestE2EHTTPEdges(t *testing.T) {
	e := newE2E(t, 1000, 1000)
	client := &http.Client{Timeout: 5 * time.Second}

	// GET (standalone SSE stream) is not served in stateless JSON mode: it
	// must answer at once, not hold a stream open.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, e.srv.URL+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 400 || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("GET /mcp = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// Body above 1 MiB is refused.
	big := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"x":"` + strings.Repeat("a", 2<<20) + `"}}`
	if code := post(t, e.srv.URL+"/mcp", big); code < 400 || code >= 500 {
		t.Errorf("2 MiB body = %d, want 4xx", code)
	}

	// Trailing slash and sub-paths are not the MCP endpoint.
	for _, p := range []string{"/mcp/", "/mcp/x"} {
		if code := post(t, e.srv.URL+p, listBody); code == http.StatusOK {
			t.Errorf("POST %s = 200", p)
		}
	}
	// /mcp is an API path: an unknown method is never the HTML site.
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodPut, e.srv.URL+"/mcp", strings.NewReader(listBody))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("PUT /mcp = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// CORS: browsers are not allowed to POST to /mcp cross-origin.
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodOptions, e.srv.URL+"/mcp", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("preflight Access-Control-Allow-Origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); strings.Contains(got, http.MethodPost) {
		t.Errorf("preflight allows POST: %q", got)
	}
}
