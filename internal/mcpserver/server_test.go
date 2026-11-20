package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testToken = "0123456789abcdef0123456789abcdef"

// fakePublic mimics the /api/v1 routes: it echoes the path, the raw path
// and the query, and answers 400 for limit=0 like the real API.
func fakePublic() http.Handler {
	r := chi.NewRouter()
	echo := func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("limit") == "0" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_limit","message":"limit must be an integer between 1 and 100"}}`)
			return
		}
		writeTestJSON(w, map[string]any{"data": map[string]string{
			"path": req.URL.Path, "query": req.URL.RawQuery, "id": chi.URLParam(req, "id"),
		}})
	}
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/now-playing", echo)
		r.Get("/history", echo)
		r.Get("/tracks/{id}", echo)
		r.Get("/stats/summary", echo)
		r.Get("/boom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	})
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"resource not found"}}`)
	})
	return r
}

func fakeAdmin() http.Handler {
	r := chi.NewRouter()
	r.Get("/overview", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, map[string]any{"version": "test"})
	})
	return r
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

type headerTransport struct{ auth string }

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if t.auth != "" {
		r.Header.Set("Authorization", t.auth)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func newTestServer(t *testing.T, adminToken string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(Options{
		Public: fakePublic(), Admin: fakeAdmin(), AdminToken: adminToken, Log: slog.New(slog.DiscardHandler),
	}))
	t.Cleanup(srv.Close)
	return srv
}

func connect(t *testing.T, srv *httptest.Server, auth string) (*mcp.ClientSession, error) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		HTTPClient:           &http.Client{Transport: headerTransport{auth: auth}},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err == nil {
		t.Cleanup(func() { _ = cs.Close() })
	}
	return cs, err
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not marked read-only", tool.Name)
		}
	}
	return names
}

func TestToolListsByAuth(t *testing.T) {
	srv := newTestServer(t, testToken)
	for _, auth := range []string{"", "Bearer", "Bearer   "} {
		cs, err := connect(t, srv, auth)
		if err != nil {
			t.Fatalf("auth %q: %v", auth, err)
		}
		names := toolNames(t, cs)
		if len(names) != len(publicTools) || slices.Contains(names, "admin_overview") {
			t.Errorf("auth %q: tools = %v; want only the %d public tools", auth, names, len(publicTools))
		}
	}

	cs, err := connect(t, srv, "Bearer "+testToken)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(t, cs)
	if len(names) != len(publicTools)+len(adminTools) || !slices.Contains(names, "admin_overview") ||
		!slices.Contains(names, "now_playing") {
		t.Errorf("admin tools = %v", names)
	}
}

func TestRejectsWrongToken(t *testing.T) {
	cases := []struct{ name, configured, auth string }{
		{"wrong token", testToken, "Bearer nope"},
		{"other scheme", testToken, "Basic Zm9vOmJhcg=="},
		{"no admin token configured", "", "Bearer " + testToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t, tc.configured)
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL,
				strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Authorization", tc.auth)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") ||
				!strings.Contains(string(body), `"unauthorized"`) {
				t.Errorf("got %d %q (WWW-Authenticate %q); want 401", resp.StatusCode, body, resp.Header.Get("WWW-Authenticate"))
			}
			if _, err := connect(t, srv, tc.auth); err == nil {
				t.Error("client connected with a rejected token")
			}
		})
	}
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestPublicToolCall(t *testing.T) {
	cs, err := connect(t, newTestServer(t, ""), "")
	if err != nil {
		t.Fatal(err)
	}
	res := callTool(t, cs, "recent_plays", map[string]any{"limit": 5, "cursor": "2026-11-01T00:00:00Z"})
	if res.IsError {
		t.Fatalf("error result: %s", resultText(res))
	}
	var body struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["path"] != "/api/v1/history" || body.Data["query"] != "cursor=2026-11-01T00%3A00%3A00Z&limit=5" {
		t.Errorf("dispatched %+v", body.Data)
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok || sc["data"] == nil {
		t.Errorf("structured content = %#v", res.StructuredContent)
	}

	// Defaults from the schema are passed through.
	res = callTool(t, cs, "listening_summary", nil)
	if !strings.Contains(resultText(res), `"query":"range=30d"`) {
		t.Errorf("summary = %s", resultText(res))
	}
}

func TestToolErrors(t *testing.T) {
	cs, err := connect(t, newTestServer(t, ""), "")
	if err != nil {
		t.Fatal(err)
	}
	// Out of range for the schema: rejected before the API is called.
	res := callTool(t, cs, "recent_plays", map[string]any{"limit": 500})
	if !res.IsError {
		t.Errorf("limit 500: want an error, got %s", resultText(res))
	}
	// Unknown enum value.
	res = callTool(t, cs, "listening_summary", map[string]any{"range": "2d"})
	if !res.IsError {
		t.Errorf("range 2d: want an error, got %s", resultText(res))
	}
	// Missing required argument.
	res = callTool(t, cs, "track_details", map[string]any{})
	if !res.IsError {
		t.Errorf("missing id: want an error, got %s", resultText(res))
	}
}

func TestCallMapsStatus(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	ctx := context.Background()
	res := call(ctx, fakePublic(), "/api/v1/history?limit=0", "t", log)
	if !res.IsError || resultText(res) != "limit must be an integer between 1 and 100" {
		t.Errorf("4xx = %v %q", res.IsError, resultText(res))
	}
	res = call(ctx, fakePublic(), "/api/v1/boom", "t", log)
	if !res.IsError || resultText(res) != "internal error" {
		t.Errorf("5xx = %v %q", res.IsError, resultText(res))
	}
	panicky := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	res = call(ctx, panicky, "/x", "t", log)
	if !res.IsError || resultText(res) != "internal error" {
		t.Errorf("panic = %v %q", res.IsError, resultText(res))
	}
}

func TestPathParamsAreEscaped(t *testing.T) {
	cs, err := connect(t, newTestServer(t, ""), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../../admin/api/overview", "a/b", "x?limit=0", "%2e%2e"} {
		res := callTool(t, cs, "track_details", map[string]any{"id": id})
		if res.IsError {
			t.Errorf("id %q: %s", id, resultText(res))
			continue
		}
		var body struct {
			Data map[string]string `json:"data"`
		}
		_ = json.Unmarshal([]byte(resultText(res)), &body)
		// The whole id stays one segment of the tracks route.
		if !strings.HasPrefix(body.Data["path"], "/api/v1/tracks/") || body.Data["id"] == "" ||
			strings.Contains(body.Data["id"], "/") || body.Data["query"] != "" {
			t.Errorf("id %q dispatched as %+v", id, body.Data)
		}
	}

	d := toolDef{path: "/top-playlists/{period}/{start}", params: []param{
		{name: "period", inPath: true, required: true}, {name: "start", inPath: true, required: true},
	}}
	u, err := d.target("", map[string]any{"period": "week", "start": "2026-11-16T00:00:00+01:00"})
	if err != nil || u != "/top-playlists/week/2026-11-16T00:00:00+01:00" {
		t.Errorf("target = %q, %v", u, err)
	}
	u, err = d.target("", map[string]any{"period": "../x", "start": "a b"})
	if err != nil || u != "/top-playlists/..%2Fx/a%20b" {
		t.Errorf("target = %q, %v", u, err)
	}
	if _, err := d.target("", map[string]any{"period": "week", "start": " "}); err == nil {
		t.Error("blank path param accepted")
	}
}

func TestAdminToolCall(t *testing.T) {
	cs, err := connect(t, newTestServer(t, testToken), "Bearer "+testToken)
	if err != nil {
		t.Fatal(err)
	}
	res := callTool(t, cs, "admin_overview", nil)
	if res.IsError || !strings.Contains(resultText(res), `"version":"test"`) {
		t.Errorf("admin_overview = %v %s", res.IsError, resultText(res))
	}
}

func TestToolSchemas(t *testing.T) {
	for _, d := range slices.Concat(publicTools, adminTools) {
		s := d.schema()
		if s.Type != "object" {
			t.Errorf("%s: type %q", d.name, s.Type)
		}
		for _, p := range d.params {
			if p.inPath && !strings.Contains(d.path, "{"+p.name+"}") {
				t.Errorf("%s: path %q lacks {%s}", d.name, d.path, p.name)
			}
		}
	}
}

// TestNullArguments sends "arguments": null over the wire (the SDK's generic
// AddTool panics on it); the tool must run with its defaults.
func TestNullArguments(t *testing.T) {
	srv := newTestServer(t, "")
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"listening_summary","arguments":null}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `range=30d`) {
		t.Errorf("got %d %s", resp.StatusCode, body)
	}
}
