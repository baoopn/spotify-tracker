package mcpserver

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"
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
	for _, d := range append(append([]toolDef{}, publicTools...), adminTools...) {
		if d.method != "" && d.name != "admin_requeue_translation" {
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
