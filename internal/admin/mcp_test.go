package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/mcpserver"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

// fakeMCPSettings stands in for *settings.Store: the admin API writes it
// and the MCP server reads it.
type fakeMCPSettings struct {
	mu      sync.Mutex
	cur     *settings.MCP
	raw     [][]byte // every saved value, as JSON
	actor   string
	touches int
}

func (f *fakeMCPSettings) MCP(context.Context) (settings.MCP, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur == nil {
		return settings.MCP{}, settings.ErrNotFound
	}
	return *f.cur, nil
}

func (f *fakeMCPSettings) SaveMCP(_ context.Context, actor string, v settings.MCP) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := json.Marshal(v)
	f.raw = append(f.raw, b)
	f.actor, f.cur = actor, &v
	return nil
}

func (f *fakeMCPSettings) TouchMCPToken(_ context.Context, hash string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur != nil && f.cur.TokenHash == hash {
		f.cur.TokenLastUsedAt = &at
		f.touches++
	}
	return nil
}

const envMCPToken = "env-token-0123456789abcdef0123456789"

type mcpHarness struct {
	st     *fakeMCPSettings
	audit  *fakeAuditor
	router chi.Router
	mcp    *httptest.Server
}

func newMCPHarness(t *testing.T) *mcpHarness {
	t.Helper()
	h := &mcpHarness{st: &fakeMCPSettings{}, audit: &fakeAuditor{}}
	cache := mcpserver.NewSettingsCache(h.st, time.Hour, nil)
	api := &API{Deps: Deps{Log: slog.New(slog.DiscardHandler), OnMCPChange: cache.Invalidate,
		MCP: MCPInfo{Enabled: true, EndpointURL: "https://listening.example.com/mcp", EnvTokenConfigured: true,
			RateLimitPerMinute: 60}},
		mcpSettings: h.st, auditor: h.audit, now: func() time.Time { return bkNow }}
	r := chi.NewRouter()
	r.Route("/admin/api", api.mountMCP)
	h.router = r

	fakeAdmin := chi.NewRouter()
	fakeAdmin.Get("/overview", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]int{}) })
	h.mcp = httptest.NewServer(mcpserver.New(mcpserver.Options{
		Public: http.NotFoundHandler(), Admin: fakeAdmin, AdminToken: envMCPToken, Settings: cache,
		Log: slog.New(slog.DiscardHandler),
	}))
	t.Cleanup(h.mcp.Close)
	return h
}

func (h *mcpHarness) do(t *testing.T, method, path, body string) (int, map[string]json.RawMessage) {
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

// tools lists the MCP tools for a bearer token; it returns the HTTP status
// and the number of admin tools.
func (h *mcpHarness) tools(t *testing.T, token string) (int, int) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, h.mcp.URL,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, 0
	}
	var out struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("tools/list: %v: %s", err, body)
	}
	n := 0
	for _, tool := range out.Result.Tools {
		if strings.HasPrefix(tool.Name, "admin_") {
			n++
		}
	}
	return resp.StatusCode, n
}

func (h *mcpHarness) generate(t *testing.T) string {
	t.Helper()
	code, body := h.do(t, http.MethodPost, "/admin/api/mcp/token", "")
	if code != http.StatusOK {
		t.Fatalf("generate: %d", code)
	}
	var tok string
	_ = json.Unmarshal(body["token"], &tok)
	if !strings.HasPrefix(tok, "stm_") || len(tok) != 4+43 {
		t.Fatalf("token %q", tok)
	}
	if string(body["prefix"]) != `"`+tok[:8]+`"` || string(body["created_at"]) != `"2026-11-09T12:00:00Z"` {
		t.Errorf("generate body = %v", body)
	}
	return tok
}

func (h *mcpHarness) lastAudit(t *testing.T) auditCall {
	t.Helper()
	if len(h.audit.calls) == 0 {
		t.Fatal("no audit entry")
	}
	return h.audit.calls[len(h.audit.calls)-1]
}

func TestMCPTokenLifecycle(t *testing.T) {
	h := newMCPHarness(t)
	admins := len(mcpserver.AdminTools())

	// No dashboard token yet: anonymous is public, the env token is admin.
	if code, n := h.tools(t, ""); code != http.StatusOK || n != 0 {
		t.Fatalf("anonymous = %d, %d admin tools", code, n)
	}
	if code, n := h.tools(t, envMCPToken); code != http.StatusOK || n != admins {
		t.Fatalf("env token = %d, %d admin tools", code, n)
	}
	if code, _ := h.tools(t, "stm_nope"); code != http.StatusUnauthorized {
		t.Fatalf("unknown token = %d", code)
	}

	tok := h.generate(t)
	if a := h.lastAudit(t); a.action != "mcp.token_generated" || a.detail["rotated"] != false ||
		a.detail["prefix"] != tok[:8] {
		t.Errorf("audit = %+v", a)
	}
	if code, n := h.tools(t, tok); code != http.StatusOK || n != admins {
		t.Fatalf("new token = %d, %d admin tools", code, n)
	}

	// Rotate: the old token stops working at once.
	tok2 := h.generate(t)
	if a := h.lastAudit(t); a.detail["rotated"] != true {
		t.Errorf("rotate audit = %+v", a)
	}
	if code, _ := h.tools(t, tok); code != http.StatusUnauthorized {
		t.Errorf("old token after rotate = %d, want 401", code)
	}
	if code, n := h.tools(t, tok2); code != http.StatusOK || n != admins {
		t.Errorf("rotated token = %d, %d", code, n)
	}

	// The plaintext is never stored, nor audited.
	for _, raw := range h.st.raw {
		if strings.Contains(string(raw), tok) || strings.Contains(string(raw), tok2) {
			t.Errorf("plaintext token stored: %s", raw)
		}
	}
	for _, c := range h.audit.calls {
		b, _ := json.Marshal(c.detail)
		if strings.Contains(string(b), tok[8:]) || strings.Contains(string(b), tok2[8:]) {
			t.Errorf("token in audit: %s", b)
		}
	}
	if h.st.cur.TokenHash != settings.HashMCPToken(tok2) || h.st.actor != "admin@example.com" {
		t.Errorf("stored = %+v by %s", h.st.cur, h.st.actor)
	}

	// Revoke: 401, and revoking again is a no-op.
	code, body := h.do(t, http.MethodDelete, "/admin/api/mcp/token", "")
	if code != http.StatusOK || string(body["token"]) != "null" {
		t.Fatalf("revoke = %d %v", code, body)
	}
	if a := h.lastAudit(t); a.action != "mcp.token_revoked" || a.detail["prefix"] != tok2[:8] {
		t.Errorf("revoke audit = %+v", a)
	}
	if code, _ := h.tools(t, tok2); code != http.StatusUnauthorized {
		t.Errorf("revoked token = %d, want 401", code)
	}
	n := len(h.audit.calls)
	if code, _ := h.do(t, http.MethodDelete, "/admin/api/mcp/token", ""); code != http.StatusOK || len(h.audit.calls) != n {
		t.Errorf("second revoke = %d (audits %d → %d)", code, n, len(h.audit.calls))
	}
}

func TestMCPAdminToolsToggle(t *testing.T) {
	h := newMCPHarness(t)
	tok := h.generate(t)

	code, body := h.do(t, http.MethodPut, "/admin/api/mcp", `{"admin_tools_enabled":false}`)
	if code != http.StatusOK || string(body["admin_tools_enabled"]) != "false" {
		t.Fatalf("put = %d %v", code, body)
	}
	if a := h.lastAudit(t); a.action != "mcp.settings_updated" || a.detail["admin_tools_enabled"] != false {
		t.Errorf("audit = %+v", a)
	}
	// Valid tokens get only the public tools; a wrong one is still 401.
	for _, tk := range []string{tok, envMCPToken} {
		if code, n := h.tools(t, tk); code != http.StatusOK || n != 0 {
			t.Errorf("disabled: token %.8s = %d, %d admin tools", tk, code, n)
		}
	}
	if code, _ := h.tools(t, "stm_wrong"); code != http.StatusUnauthorized {
		t.Errorf("disabled: wrong token = %d, want 401", code)
	}
	// The token survives the toggle.
	if h.st.cur.TokenHash == "" {
		t.Error("toggle dropped the token")
	}

	if code, _ := h.do(t, http.MethodPut, "/admin/api/mcp", `{}`); code != http.StatusUnprocessableEntity {
		t.Errorf("missing field = %d, want 422", code)
	}
	if code, _ := h.do(t, http.MethodPut, "/admin/api/mcp", `{"admin_tools_enabled":true,"x":1}`); code != http.StatusBadRequest {
		t.Errorf("unknown field = %d, want 400", code)
	}
}

func TestGetMCP(t *testing.T) {
	h := newMCPHarness(t)
	code, body := h.do(t, http.MethodGet, "/admin/api/mcp", "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for k, want := range map[string]string{
		"enabled": "true", "endpoint_url": `"https://listening.example.com/mcp"`, "admin_tools_enabled": "true",
		"token": "null", "env_token_configured": "true", "rate_limit_per_minute": "60",
	} {
		if got := string(body[k]); got != want {
			t.Errorf("%s = %s, want %s", k, got, want)
		}
	}
	var tools []mcpserver.ToolInfo
	_ = json.Unmarshal(body["admin_tools"], &tools)
	if len(tools) != len(mcpserver.AdminTools()) || tools[0].Name != "admin_overview" || tools[0].Description == "" {
		t.Errorf("admin_tools = %s", body["admin_tools"])
	}

	tok := h.generate(t)
	if code, _ := h.tools(t, tok); code != http.StatusOK {
		t.Fatal(code)
	}
	_, body = h.do(t, http.MethodGet, "/admin/api/mcp", "")
	var view mcpTokenView
	_ = json.Unmarshal(body["token"], &view)
	if !view.Configured || view.Prefix != tok[:8] || view.CreatedAt == nil {
		t.Errorf("token = %s", body["token"])
	}
}
