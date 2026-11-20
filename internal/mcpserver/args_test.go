package mcpserver

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

	"github.com/go-chi/chi/v5"
)

// echoed decodes the fake handler's {"data":{path,query,id}} result.
func echoed(t *testing.T, text string) map[string]string {
	t.Helper()
	var body struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("result %q: %v", text, err)
	}
	return body.Data
}

// TestArgumentValidation: arguments outside the schema are rejected before
// the API is called, with a tool error (never a protocol error or a 500).
func TestArgumentValidation(t *testing.T) {
	cs, err := connect(t, newTestServer(t, testToken), "Bearer "+testToken)
	if err != nil {
		t.Fatal(err)
	}
	long := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"limit zero", "recent_plays", map[string]any{"limit": 0}},
		{"limit negative", "top_tracks", map[string]any{"limit": -1}},
		{"limit above max", "top_artists", map[string]any{"limit": 101}},
		{"limit fraction", "top_genres", map[string]any{"limit": 5.5}},
		{"limit as string", "recent_plays", map[string]any{"limit": "5"}},
		{"limit as bool", "recent_plays", map[string]any{"limit": true}},
		{"limit huge", "recent_plays", map[string]any{"limit": 1e300}},
		{"recent playlists above 50", "recent_playlists", map[string]any{"limit": 51}},
		{"top playlists above 24", "top_playlists", map[string]any{"limit": 25}},
		{"range wrong case", "listening_summary", map[string]any{"range": "30D"}},
		{"range empty", "audio_profile", map[string]any{"range": ""}},
		{"range as number", "top_tracks", map[string]any{"range": 30}},
		{"cursor too long", "recent_plays", map[string]any{"cursor": long(65)}},
		{"cursor as object", "recent_plays", map[string]any{"cursor": map[string]any{"a": 1}}},
		{"id empty", "track_details", map[string]any{"id": ""}},
		{"id blank", "track_details", map[string]any{"id": "   "}},
		{"id null", "track_details", map[string]any{"id": nil}},
		{"id as number", "track_details", map[string]any{"id": 123}},
		{"id too long", "track_details", map[string]any{"id": long(65)}},
		{"admin q too long", "admin_history", map[string]any{"q": long(201)}},
		{"admin status unknown", "admin_tracks", map[string]any{"status": "deleted"}},
		{"admin sort unknown", "admin_tracks", map[string]any{"sort": "plays; DROP TABLE"}},
		{"admin order unknown", "admin_artists", map[string]any{"order": "up"}},
		{"admin offset negative", "admin_playlists", map[string]any{"offset": -1}},
		{"admin offset above max", "admin_playlists", map[string]any{"offset": 1_000_001}},
		{"admin period unknown", "admin_top_playlists", map[string]any{"period": "day"}},
		{"admin top playlist period missing", "admin_top_playlist", map[string]any{"start": "2026-11-16T00:00:00Z"}},
		{"admin top playlist period traversal", "admin_top_playlist",
			map[string]any{"period": "../settings", "start": "x"}},
		{"admin analytics range", "admin_api_analytics", map[string]any{"range": "90d"}},
		{"admin tz too long", "admin_page_analytics", map[string]any{"tz": long(65)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callTool(t, cs, tc.tool, tc.args)
			if !res.IsError {
				t.Errorf("%s %v: want a tool error, got %s", tc.tool, tc.args, resultText(res))
			}
			if res.StructuredContent != nil {
				t.Errorf("error result carries structured content %#v", res.StructuredContent)
			}
		})
	}
}

// TestArgumentsPassThrough: valid arguments, at the bounds, reach the API as
// query parameters; unknown arguments are dropped (never forwarded), and
// nulls fall back to the API defaults.
func TestArgumentsPassThrough(t *testing.T) {
	cs, err := connect(t, newTestServer(t, ""), "")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, tool string
		args       map[string]any
		path, qry  string
	}{
		{"limit min", "recent_plays", map[string]any{"limit": 1}, "/api/v1/history", "limit=1"},
		{"limit max", "recent_plays", map[string]any{"limit": 100}, "/api/v1/history", "limit=100"},
		{"limit integral float", "recent_plays", map[string]any{"limit": 7.0}, "/api/v1/history", "limit=7"},
		{"defaults", "recent_plays", nil, "/api/v1/history", "limit=20"},
		{"unknown args dropped", "recent_plays",
			map[string]any{"limit": 5, "path": "/admin/api/overview", "range": "7d", "q": "x"},
			"/api/v1/history", "limit=5"},
		{"empty cursor left out", "recent_plays", map[string]any{"cursor": "  "}, "/api/v1/history", "limit=20"},
		{"query chars escaped", "recent_plays", map[string]any{"cursor": "a&limit=0#x"},
			"/api/v1/history", "cursor=a%26limit%3D0%23x&limit=20"},
		{"all range", "listening_summary", map[string]any{"range": "all"}, "/api/v1/stats/summary", "range=all"},
		{"no args tool ignores args", "now_playing", map[string]any{"limit": 5}, "/api/v1/now-playing", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callTool(t, cs, tc.tool, tc.args)
			if res.IsError {
				t.Fatalf("error: %s", resultText(res))
			}
			got := echoed(t, resultText(res))
			if got["path"] != tc.path || got["query"] != tc.qry {
				t.Errorf("dispatched %s?%s; want %s?%s", got["path"], got["query"], tc.path, tc.qry)
			}
		})
	}
}

// TestNullOptionalArgument: an optional argument sent as null (common from
// LLM clients) means "not set"; a null required argument is still an error.
func TestNullOptionalArgument(t *testing.T) {
	cs, err := connect(t, newTestServer(t, ""), "")
	if err != nil {
		t.Fatal(err)
	}
	res := callTool(t, cs, "recent_plays", map[string]any{"cursor": nil, "limit": nil})
	if res.IsError {
		t.Fatalf("null optional arguments rejected: %s", resultText(res))
	}
	if got := echoed(t, resultText(res)); got["query"] != "limit=20" {
		t.Errorf("dispatched %+v", got)
	}
	if res := callTool(t, cs, "track_details", map[string]any{"id": nil}); !res.IsError {
		t.Errorf("null required argument accepted: %s", resultText(res))
	}
}

// postRPC sends one raw JSON-RPC message to the MCP endpoint.
func postRPC(t *testing.T, url, auth, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestMalformedArgumentsOverTheWire: arguments that are not an object, and
// calls of unknown or admin-only tools, fail cleanly (no 5xx, no panic).
func TestMalformedArgumentsOverTheWire(t *testing.T) {
	srv := newTestServer(t, testToken)
	call := func(name, args string) string {
		return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
	}
	for _, tc := range []struct{ name, body string }{
		{"array", call("recent_plays", `[1,2]`)},
		{"string", call("recent_plays", `"limit=5"`)},
		{"number", call("recent_plays", `5`)},
		{"true", call("listening_summary", `true`)},
		{"nested null id", call("track_details", `{"id":null}`)},
		{"unknown tool", call("delete_everything", `{}`)},
		{"admin tool anonymously", call("admin_overview", `{}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := postRPC(t, srv.URL, "", tc.body)
			if code >= 500 {
				t.Fatalf("status %d: %s", code, body)
			}
			ok := strings.Contains(body, `"isError":true`) || strings.Contains(body, `"error":`)
			if !ok || strings.Contains(body, `"version":"test"`) {
				t.Errorf("want a tool or protocol error, got %d %s", code, body)
			}
		})
	}
}

// adminRoutesFake has the list and item routes side by side, to show that
// path arguments can never switch to a sibling route.
func adminRoutesFake() http.Handler {
	r := chi.NewRouter()
	echo := func(route string) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			writeTestJSON(w, map[string]any{"data": map[string]string{
				"route": route, "path": req.URL.Path, "query": req.URL.RawQuery,
				"id": chi.URLParam(req, "id"), "start": chi.URLParam(req, "start"),
			}})
		}
	}
	r.Get("/overview", echo("overview"))
	r.Get("/tracks", echo("tracks"))
	r.Get("/tracks/{id}", echo("track"))
	r.Get("/top-playlists", echo("top-playlists"))
	r.Get("/top-playlists/{period}/{start}", echo("top-playlist"))
	r.Get("/settings/email", echo("SETTINGS"))
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	return r
}

// TestAdminPathParamsStayInRoute: admin path arguments, including dot
// segments and encoded slashes, always hit the item route.
func TestAdminPathParamsStayInRoute(t *testing.T) {
	srv := httptest.NewServer(New(Options{Public: fakePublic(), Admin: adminRoutesFake(), AdminToken: testToken,
		Log: slog.New(slog.DiscardHandler)}))
	t.Cleanup(srv.Close)
	cs, err := connect(t, srv, "Bearer "+testToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"..", ".", "../overview", "../../settings/email", "..%2Fsettings%2Femail",
		"a/../../overview", "x?status=failed", "x#frag", `..\settings`, "%00", "\x00"} {
		res := callTool(t, cs, "admin_track", map[string]any{"id": id})
		if res.IsError {
			// A rejected id is fine too; only reaching another route is not.
			continue
		}
		got := echoed(t, resultText(res))
		if got["route"] != "track" || got["query"] != "" || strings.Contains(got["id"], "/") {
			t.Errorf("id %q reached %+v", id, got)
		}
	}
	for _, start := range []string{"..", "../../settings/email", "2026-11-16T00:00:00Z/../x"} {
		res := callTool(t, cs, "admin_top_playlist", map[string]any{"period": "week", "start": start})
		if res.IsError {
			continue
		}
		got := echoed(t, resultText(res))
		if got["route"] != "top-playlist" || strings.Contains(got["start"], "/") {
			t.Errorf("start %q reached %+v", start, got)
		}
	}
}

// TestCallMapsMoreStatuses covers the remaining status mappings.
func TestCallMapsMoreStatuses(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	h := func(status int, body string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if status != 0 {
				w.WriteHeader(status)
			}
			_, _ = io.WriteString(w, body)
		})
	}
	cases := []struct {
		name    string
		h       http.Handler
		isError bool
		text    string
	}{
		{"implicit 200", h(0, `{"a":1}`), false, `{"a":1}`},
		{"201 JSON", h(http.StatusCreated, `[1]`), false, `[1]`},
		{"200 invalid JSON", h(http.StatusOK, `not json`), true, "internal error"},
		{"200 empty body", h(http.StatusOK, ``), true, "internal error"},
		{"404 API error", h(http.StatusNotFound, `{"error":{"message":"track not found"}}`), true, "track not found"},
		{"422 with fields", h(http.StatusUnprocessableEntity,
			`{"error":{"message":"invalid","fields":{"b":"bad","a":"worse"}}}`), true, "invalid; a: worse; b: bad"},
		{"400 non-JSON", h(http.StatusBadRequest, `oops`), true, "Bad Request"},
		{"429 empty", h(http.StatusTooManyRequests, ``), true, "Too Many Requests"},
		{"3xx", h(http.StatusFound, ``), true, "internal error"},
		{"500 with details", h(http.StatusInternalServerError,
			`{"error":{"message":"pq: password authentication failed"}}`), true, "internal error"},
		{"503", h(http.StatusServiceUnavailable, `{"error":{"message":"redis down"}}`), true, "internal error"},
		{"panic with error", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(io.ErrUnexpectedEOF)
		}), true, "internal error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := call(t.Context(), tc.h, "/x", "t", log)
			if res.IsError != tc.isError || resultText(res) != tc.text {
				t.Errorf("got %v %q; want %v %q", res.IsError, resultText(res), tc.isError, tc.text)
			}
		})
	}

	// http.ErrAbortHandler is recovered too: tools run on an SDK goroutine,
	// where a re-raised panic would crash the process.
	res := call(t.Context(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }),
		"/x", "t", log)
	if !res.IsError || resultText(res) != "internal error" {
		t.Errorf("ErrAbortHandler = %v %q; want internal error", res.IsError, resultText(res))
	}
}

// TestPublicToolsStayOnReadRoutes: no public tool can reach the SSE stream
// or the analytics beacon, and every tool path is a plain relative path.
func TestPublicToolsStayOnReadRoutes(t *testing.T) {
	for _, d := range publicTools {
		for _, banned := range []string{"stream", "events", "admin", "..", "?", "#"} {
			if strings.Contains(d.path, banned) {
				t.Errorf("public tool %s maps to %s", d.name, d.path)
			}
		}
	}
	for _, d := range adminTools {
		for _, banned := range []string{"settings", "auth", "audit", "config", "mcp", "callback", "..", "?"} {
			if strings.Contains(d.path, banned) {
				t.Errorf("admin tool %s maps to %s", d.name, d.path)
			}
		}
	}
}

// TestToolCallBehindChiRouter mounts /mcp on an outer chi router, as
// httpapi.NewRouter does. The synthetic GET in call() must not inherit the
// outer request's chi route context (RouteMethod "POST", RoutePath "/mcp"),
// or the inner chi router reuses it and answers 405/404 for every tool. The
// outer route pattern must stay "/mcp" (access log and metrics label).
func TestToolCallBehindChiRouter(t *testing.T) {
	var mu sync.Mutex
	var patterns []string
	outer := chi.NewRouter()
	outer.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			mu.Lock()
			patterns = append(patterns, chi.RouteContext(r.Context()).RoutePattern())
			mu.Unlock()
		})
	})
	outer.Handle("/mcp", New(Options{Public: fakePublic(), Admin: fakeAdmin(), AdminToken: testToken,
		Log: slog.New(slog.DiscardHandler)}))
	srv := httptest.NewServer(outer)
	t.Cleanup(srv.Close)

	srv.URL += "/mcp"
	cs, err := connect(t, srv, "Bearer "+testToken)
	if err != nil {
		t.Fatal(err)
	}
	if res := callTool(t, cs, "now_playing", nil); res.IsError {
		t.Errorf("public tool behind chi = %s", resultText(res))
	}
	if res := callTool(t, cs, "admin_overview", nil); res.IsError {
		t.Errorf("admin tool behind chi = %s", resultText(res))
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range patterns {
		if p != "/mcp" {
			t.Errorf("outer route pattern = %q, want /mcp", p)
		}
	}
}

// TestChiRouteContextIsTheCause is the control for TestToolCallBehindChiRouter:
// dropping the inherited chi route context in front of the inner handlers
// makes the same setup work, which pins the cause.
func TestChiRouteContextIsTheCause(t *testing.T) {
	strip := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, nil)))
		})
	}
	outer := chi.NewRouter()
	outer.Handle("/mcp", New(Options{Public: strip(fakePublic()), Admin: strip(fakeAdmin()), AdminToken: testToken,
		Log: slog.New(slog.DiscardHandler)}))
	srv := httptest.NewServer(outer)
	t.Cleanup(srv.Close)
	srv.URL += "/mcp"
	cs, err := connect(t, srv, "Bearer "+testToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"now_playing", "admin_overview"} {
		if res := callTool(t, cs, name, nil); res.IsError {
			t.Errorf("%s with the route context dropped = %s", name, resultText(res))
		}
	}
}
