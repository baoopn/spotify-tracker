package notify_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/alerts"
	"github.com/baoopn/spotify-tracker/internal/notify"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

// TestGolden compares every alert email with testdata/<name>.{html,txt}.
// Regenerate with: go test ./internal/notify -run TestGolden -update
func TestGolden(t *testing.T) {
	const publicURL = "https://listening.example.com"
	b, err := notify.NewBrand(publicURL, "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, s := range alerts.Samples(publicURL, now) {
		text, html, err := s.Email.Render(b)
		if err != nil {
			t.Fatal(err)
		}
		if len(html) > 30*1024 {
			t.Errorf("%s: HTML is %d bytes", s.Name, len(html))
		}
		for ext, got := range map[string]string{".html": html, ".txt": text} {
			path := filepath.Join("testdata", s.Name+ext)
			if *update {
				if err := os.MkdirAll("testdata", 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create)", err)
			}
			if string(want) != got {
				t.Errorf("%s differs from the golden file; run go test ./internal/notify -run TestGolden -update and review the diff", path)
			}
		}
	}
}
