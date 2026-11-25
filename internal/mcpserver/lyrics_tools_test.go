package mcpserver

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func findTool(t *testing.T, defs []toolDef, name string) toolDef {
	t.Helper()
	for _, d := range defs {
		if d.name == name {
			return d
		}
	}
	t.Fatalf("tool %s not found", name)
	return toolDef{}
}

// Lyrics are admin-only: no public tool may reach them.
func TestNoPublicLyricsTools(t *testing.T) {
	for _, d := range publicTools {
		if strings.Contains(d.name, "lyric") || strings.Contains(d.path, "lyric") {
			t.Errorf("public tool %s (%s) serves lyrics", d.name, d.path)
		}
	}
}

func TestLyricsToolTargets(t *testing.T) {
	search := findTool(t, adminTools, "admin_search_lyrics")
	u, err := search.target("/admin/api", map[string]any{"q": "anh mơ", "limit": float64(10)})
	if err != nil {
		t.Fatal(err)
	}
	if u != "/admin/api/lyrics/search?limit=10&q=anh+m%C6%A1" {
		t.Errorf("search target = %s", u)
	}
	if _, err := search.target("/admin/api", map[string]any{}); err == nil {
		t.Error("q must be required")
	}

	track := findTool(t, adminTools, "admin_track_lyrics")
	u, err = track.target("/admin/api", map[string]any{"id": "a/b c"})
	if err != nil {
		t.Fatal(err)
	}
	// synced is left out by default; the API defaults to false.
	if u != "/admin/api/tracks/a%2Fb%20c/lyrics" {
		t.Errorf("track target = %s", u)
	}
	u, _ = track.target("/admin/api", map[string]any{"id": "x", "synced": "true"})
	if u != "/admin/api/tracks/x/lyrics?synced=true" {
		t.Errorf("synced target = %s", u)
	}
}

// The re-queue tool POSTs to the admin route; every other tool is a GET.
func TestTranslationToolTargets(t *testing.T) {
	rq := findTool(t, adminTools, "admin_requeue_translation")
	u, err := rq.target("/admin/api", map[string]any{"id": "t 1"})
	if err != nil || u != "/admin/api/tracks/t%201/translation/requeue" || rq.method != "POST" {
		t.Errorf("requeue = %s %s, %v", rq.method, u, err)
	}
	q := findTool(t, adminTools, "admin_translation_queue")
	if u, _ := q.target("/admin/api", map[string]any{}); u != "/admin/api/lyrics/translation-queue" || q.method != "" {
		t.Errorf("queue = %s %s", q.method, u)
	}
	lr := findTool(t, adminTools, "admin_requeue_lyrics")
	if u, err := lr.target("/admin/api", map[string]any{"id": "t 1"}); err != nil ||
		u != "/admin/api/tracks/t%201/lyrics/requeue" || lr.method != "POST" {
		t.Errorf("lyrics requeue = %s %s, %v", lr.method, u, err)
	}
	lq := findTool(t, adminTools, "admin_lyrics_queue")
	if u, _ := lq.target("/admin/api", map[string]any{}); u != "/admin/api/lyrics/queue" || lq.method != "" {
		t.Errorf("lyrics queue = %s %s", lq.method, u)
	}
	for _, d := range append(append([]toolDef{}, publicTools...), adminTools...) {
		if d.method != "" && !writeTools[d.name] {
			t.Errorf("unexpected write tool %s", d.name)
		}
	}
}

// call sends the tool's method.
func TestCallMethod(t *testing.T) {
	var got string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method
		_, _ = w.Write([]byte(`{"queued":true}`))
	})
	log := slog.New(slog.DiscardHandler)
	if res := call(t.Context(), h, "POST", "/x", "t", log); res.IsError || got != "POST" {
		t.Errorf("POST: %v %s", res.IsError, got)
	}
	if call(t.Context(), h, "", "/x", "t", log); got != "GET" {
		t.Errorf("default method = %s", got)
	}
}

// writeTools are the only tools that are not GETs.
var writeTools = map[string]bool{
	"admin_requeue_translation": true, "admin_requeue_lyrics": true,
	"admin_set_lyrics": true, "admin_clear_manual_lyrics": true,
}

// The manual lyrics tools send their arguments as a JSON body; strings are
// kept as given (not trimmed), and only the id goes into the path.
func TestManualLyricsTools(t *testing.T) {
	set := findTool(t, adminTools, "admin_set_lyrics")
	args := map[string]any{"id": "t 1", "synced": "[00:01.00] a  \n", "instrumental": false}
	u, err := set.target("/admin/api", args)
	if err != nil || u != "/admin/api/tracks/t%201/lyrics" || set.method != "PUT" || !set.destructive {
		t.Errorf("set = %s %s, %v", set.method, u, err)
	}
	if got := string(set.body(args)); got != `{"instrumental":false,"synced":"[00:01.00] a  \n"}` {
		t.Errorf("set body = %s", got)
	}
	if got := string(set.body(map[string]any{"id": "x"})); got != `{}` {
		t.Errorf("empty body = %s", got)
	}
	clr := findTool(t, adminTools, "admin_clear_manual_lyrics")
	if u, err := clr.target("/admin/api", map[string]any{"id": "x"}); err != nil ||
		u != "/admin/api/tracks/x/lyrics/manual" || clr.method != "DELETE" || !clr.destructive || clr.body(nil) != nil {
		t.Errorf("clear = %s %s, %v", clr.method, u, err)
	}
	rq := findTool(t, adminTools, "admin_requeue_lyrics")
	if !rq.destructive {
		t.Error("admin_requeue_lyrics can replace manual lyrics: it must be marked destructive")
	}
	if got := string(rq.body(map[string]any{"id": "x", "replace_manual": true})); got != `{"replace_manual":true}` {
		t.Errorf("requeue body = %s", got)
	}
	s := set.schema()
	if s.Properties["instrumental"].Type != "boolean" || s.Properties["synced"].Type != "string" {
		t.Errorf("schema = %+v", s.Properties)
	}
}

// callBody sends the JSON body; LRC line errors reach the tool error text.
func TestCallBodyAndLineErrors(t *testing.T) {
	var got, ctype string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, ctype = string(b), r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_lrc","message":"the synced lyrics have 2 errors",` +
			`"lines":[{"line":3,"message":"out of order"},{"line":0,"message":"no timed lines"}]}}`))
	})
	res := callBody(t.Context(), h, "PUT", "/x", []byte(`{"plain":"a"}`), "t", slog.New(slog.DiscardHandler))
	if got != `{"plain":"a"}` || ctype != "application/json" || !res.IsError {
		t.Fatalf("body %q type %q error %v", got, ctype, res.IsError)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if text != "the synced lyrics have 2 errors; line 3: out of order; no timed lines" {
		t.Errorf("error = %q", text)
	}
}
