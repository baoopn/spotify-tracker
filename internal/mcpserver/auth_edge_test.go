package mcpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/settings"
)

// rawList posts tools/list with the given Authorization header values (none
// when empty) and returns the status, the number of admin tools and the
// response headers.
func rawList(t *testing.T, url string, auth ...string) (int, int, http.Header) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, url,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if len(auth) > 0 {
		req.Header["Authorization"] = auth
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, 0, resp.Header
	}
	var out struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("tools/list: %v: %s", err, body)
	}
	if len(out.Result.Tools) == 0 {
		t.Fatalf("tools/list returned no tools: %s", body)
	}
	n := 0
	for _, tool := range out.Result.Tools {
		if strings.HasPrefix(tool.Name, "admin_") {
			n++
		}
	}
	return resp.StatusCode, n, resp.Header
}

// TestBearerParsing covers the Authorization header forms: the scheme is
// case-insensitive, spaces around the token are ignored, and anything that
// is not "Bearer <token>" is rejected (never downgraded to anonymous).
func TestBearerParsing(t *testing.T) {
	srv := newTestServer(t, testToken)
	admins := len(adminTools)
	cases := []struct {
		name, auth  string
		code, admin int
	}{
		{"lower-case scheme", "bearer " + testToken, http.StatusOK, admins},
		{"upper-case scheme", "BEARER " + testToken, http.StatusOK, admins},
		{"extra spaces", "  Bearer    " + testToken + "   ", http.StatusOK, admins},
		{"lower-case bare scheme", "bearer", http.StatusOK, 0},
		{"only spaces", "    ", http.StatusOK, 0},
		{"tab separator", "Bearer\t" + testToken, http.StatusUnauthorized, 0},
		{"colon after scheme", "Bearer: " + testToken, http.StatusUnauthorized, 0},
		{"token with inner space", "Bearer " + testToken[:16] + " " + testToken[16:], http.StatusUnauthorized, 0},
		{"token prefix only", "Bearer " + testToken[:31], http.StatusUnauthorized, 0},
		{"token plus suffix", "Bearer " + testToken + "x", http.StatusUnauthorized, 0},
		{"token case changed", "Bearer " + strings.ToUpper(testToken), http.StatusUnauthorized, 0},
		{"scheme without space", "Bearer" + testToken, http.StatusUnauthorized, 0},
		{"Token scheme", "Token " + testToken, http.StatusUnauthorized, 0},
		{"Basic scheme", "Basic Zm9vOmJhcg==", http.StatusUnauthorized, 0},
		{"Basic scheme alone", "Basic", http.StatusUnauthorized, 0},
		{"very long token", "Bearer " + strings.Repeat("a", 64<<10), http.StatusUnauthorized, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, n, hdr := rawList(t, srv.URL, tc.auth)
			if code != tc.code || n != tc.admin {
				t.Fatalf("got %d with %d admin tools; want %d with %d", code, n, tc.code, tc.admin)
			}
			if code == http.StatusUnauthorized {
				if !strings.HasPrefix(hdr.Get("WWW-Authenticate"), "Bearer") ||
					hdr.Get("Cache-Control") != "no-store" {
					t.Errorf("401 headers = %v", hdr)
				}
			}
		})
	}
}

// TestMultipleAuthorizationHeaders: only the first header is read. Sending
// a second header never adds the admin tools, and a wrong first token is
// rejected even when a valid one follows.
func TestMultipleAuthorizationHeaders(t *testing.T) {
	srv := newTestServer(t, testToken)
	if code, _, _ := rawList(t, srv.URL, "Bearer nope", "Bearer "+testToken); code != http.StatusUnauthorized {
		t.Errorf("wrong then valid = %d, want 401", code)
	}
	if code, n, _ := rawList(t, srv.URL, "Bearer", "Bearer "+testToken); code != http.StatusOK || n != 0 {
		t.Errorf("anonymous then valid = %d with %d admin tools; want public only", code, n)
	}
	if code, _, _ := rawList(t, srv.URL, "Basic Zm9v", "Bearer "+testToken); code != http.StatusUnauthorized {
		t.Errorf("basic then valid = %d, want 401", code)
	}
}

// TestNoAdminHandler: without Options.Admin a valid token gets the public
// server, and a wrong token is still 401.
func TestNoAdminHandler(t *testing.T) {
	srv := httptest.NewServer(New(Options{Public: fakePublic(), AdminToken: testToken}))
	t.Cleanup(srv.Close)
	if code, n, _ := rawList(t, srv.URL, "Bearer "+testToken); code != http.StatusOK || n != 0 {
		t.Errorf("valid token without admin handler = %d, %d admin tools", code, n)
	}
	if code, _, _ := rawList(t, srv.URL, "Bearer nope"); code != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", code)
	}
}

// TestEnvAndStoredTokens: with both tokens configured each one works on its
// own, revoking the stored one leaves the env token working, the toggle
// applies to both, and the hash of the env token is not accepted in its
// place.
func TestEnvAndStoredTokens(t *testing.T) {
	f := &fakeSettings{cur: settings.MCP{AdminToolsEnabled: true, TokenHash: settings.HashMCPToken(storedToken)}}
	c, srv, _ := newCacheServer(t, f, testToken)
	admins := len(adminTools)

	for _, tok := range []string{testToken, storedToken} {
		if code, n, _ := rawList(t, srv.URL, "Bearer "+tok); code != http.StatusOK || n != admins {
			t.Errorf("token %.8s = %d, %d admin tools", tok, code, n)
		}
	}
	// The stored hash itself is not a token.
	if code, _, _ := rawList(t, srv.URL, "Bearer "+settings.HashMCPToken(storedToken)); code != http.StatusUnauthorized {
		t.Errorf("hash as token = %d, want 401", code)
	}

	// Admin tools off: both valid tokens get the public tools only.
	f.mu.Lock()
	f.cur.AdminToolsEnabled = false
	f.mu.Unlock()
	c.Invalidate()
	for _, tok := range []string{testToken, storedToken} {
		if code, n, _ := rawList(t, srv.URL, "Bearer "+tok); code != http.StatusOK || n != 0 {
			t.Errorf("disabled: token %.8s = %d, %d admin tools; want public only", tok, code, n)
		}
	}
	if code, _, _ := rawList(t, srv.URL, "Bearer stm_wrong"); code != http.StatusUnauthorized {
		t.Errorf("disabled: wrong token = %d, want 401", code)
	}

	// Revoke the stored token: env still works, stored is 401.
	f.mu.Lock()
	f.cur = settings.MCP{AdminToolsEnabled: true}
	f.mu.Unlock()
	c.Invalidate()
	if code, n, _ := rawList(t, srv.URL, "Bearer "+testToken); code != http.StatusOK || n != admins {
		t.Errorf("env after revoke = %d, %d", code, n)
	}
	if code, _, _ := rawList(t, srv.URL, "Bearer "+storedToken); code != http.StatusUnauthorized {
		t.Errorf("stored after revoke = %d, want 401", code)
	}
	c.wg.Wait()
}

// TestAnonymousSkipsSettings: anonymous calls never load the settings, so
// a Postgres outage cannot break the public tools.
func TestAnonymousSkipsSettings(t *testing.T) {
	f := &fakeSettings{err: io.ErrUnexpectedEOF}
	_, srv, _ := newCacheServer(t, f, "")
	if code, n, _ := rawList(t, srv.URL); code != http.StatusOK || n != 0 {
		t.Errorf("anonymous = %d, %d", code, n)
	}
	if f.loads != 0 {
		t.Errorf("anonymous request loaded the settings %d times", f.loads)
	}
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestFailedAuthLogNoToken: a rejected token is logged at warn, without the
// token itself.
func TestFailedAuthLogNoToken(t *testing.T) {
	var buf syncBuffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := httptest.NewServer(New(Options{Public: fakePublic(), Admin: fakeAdmin(), AdminToken: testToken, Log: log}))
	t.Cleanup(srv.Close)

	const secret = "stm_this-must-never-be-logged-0123456789"
	if code, _, _ := rawList(t, srv.URL, "Bearer "+secret); code != http.StatusUnauthorized {
		t.Fatalf("status %d", code)
	}
	// A valid token is never logged either.
	if code, _, _ := rawList(t, srv.URL, "Bearer "+testToken); code != http.StatusOK {
		t.Fatalf("valid status %d", code)
	}
	logs := buf.String()
	if strings.Contains(logs, secret) || strings.Contains(logs, "this-must-never") || strings.Contains(logs, testToken) {
		t.Errorf("token in logs: %s", logs)
	}
	if !strings.Contains(logs, `"level":"WARN"`) || !strings.Contains(logs, "mcp authentication failed") {
		t.Errorf("no warn entry for the failed auth: %s", logs)
	}
}
