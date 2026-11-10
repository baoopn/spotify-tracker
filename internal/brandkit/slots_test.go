package brandkit

import (
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"

	publicui "github.com/baoopn/spotify-tracker/web/public"
)

func TestSlotsTable(t *testing.T) {
	ss := Slots()
	if len(ss) != 31 {
		t.Fatalf("got %d slots, want 31", len(ss))
	}
	names, fallbacks := map[string]bool{}, map[string]bool{}
	var roots []string
	for _, s := range ss {
		if names[s.Name] || fallbacks[s.Fallback] {
			t.Errorf("duplicate slot %+v", s)
		}
		names[s.Name], fallbacks[s.Fallback] = true, true
		if path.Ext(s.Fallback) != s.Kind.ext() {
			t.Errorf("%s: fallback %s does not match kind %d", s.Name, s.Fallback, s.Kind)
		}
		if got, ok := SlotByName(s.Name); !ok || got != s {
			t.Errorf("SlotByName(%q) = %+v, %v", s.Name, got, ok)
		}
		if s.Root {
			roots = append(roots, s.Fallback)
		}
	}
	want := "/favicon.ico /favicon.svg /apple-touch-icon.png /favicon-16.png /favicon-32.png /favicon-48.png " +
		"/icon-192.png /icon-512.png /icon-maskable-512.png /og-image.png"
	if got := strings.Join(roots, " "); got != want {
		t.Errorf("root files = %s\nwant %s", got, want)
	}
	for name, fb := range map[string]string{
		"mark_primary":                  "/logo/listening-mark-primary.svg",
		"wordmark_cream":                "/logo/listening-wordmark-cream.svg",
		"icon_now_playing_navy":         "/icons/now-playing-navy.svg",
		"icon_now_playing_static_cream": "/icons/now-playing-static-cream.svg",
		"og_image":                      "/og-image.png",
		EmailLogoSlot:                   "/email/listening-wordmark-primary@2x.png",
	} {
		if s, _ := SlotByName(name); s.Fallback != fb {
			t.Errorf("%s fallback = %q, want %q", name, s.Fallback, fb)
		}
	}
	if _, ok := SlotByName("nope"); ok {
		t.Error("unknown slot found")
	}
	ss[0].Name = "changed"
	if Slots()[0].Name == "changed" {
		t.Error("Slots does not return a copy")
	}
}

func TestFallbacksExist(t *testing.T) {
	check := func(t *testing.T, fsys fs.FS) {
		for _, s := range Slots() {
			if _, err := fs.Stat(fsys, strings.TrimPrefix(s.Fallback, "/")); err != nil {
				t.Errorf("%s: fallback %s missing: %v", s.Name, s.Fallback, err)
			}
		}
	}
	t.Run("static", func(t *testing.T) { check(t, os.DirFS("../../web/public/static")) })
	t.Run("embedded", func(t *testing.T) {
		dist := publicui.FS()
		if _, err := fs.Stat(dist, "index.html"); err != nil {
			t.Skip("public site not built (web/public/build is empty)")
		}
		check(t, dist)
	})
}

func TestKindAccepts(t *testing.T) {
	cases := []struct {
		k    Kind
		ct   string
		want bool
	}{
		{SVG, "image/svg+xml", true},
		{SVG, "Image/SVG+XML; charset=utf-8", true},
		{SVG, "image/png", false},
		{PNG, "image/png", true},
		{PNG, "text/html", false},
		{ICO, "image/x-icon", true},
		{ICO, "image/vnd.microsoft.icon", true},
		{ICO, "image/png", false},
		{PNG, "", false},
		{PNG, ";;", false},
	}
	for _, c := range cases {
		if got := c.k.accepts(c.ct); got != c.want {
			t.Errorf("kind %d accepts(%q) = %v, want %v", c.k, c.ct, got, c.want)
		}
	}
}
