package alerts

import (
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/notify"
)

func TestListenRequestEmailEscapes(t *testing.T) {
	b, _ := notify.NewBrand("https://listening.example.com", "")
	name := "Eve <script>alert(1)</script>\r\nBcc: victim@example.com"
	e := ListenRequestEmail(name, "eve@example.com", "<b>hi</b> & bye", "VN",
		time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC), "https://listening.example.com/admin/listeners")
	msg, err := notify.Message("alerts@example.com", []string{"owner@example.com"}, e, b, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw := string(msg)
	head, _, _ := strings.Cut(raw, "\r\n\r\n")
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Errorf("header injection: %q", line)
		}
	}
	_, html, err := e.Render(b)
	if err != nil || strings.Contains(html, "<script>") || strings.Contains(html, "<b>hi</b>") ||
		!strings.Contains(html, "&lt;b&gt;hi&lt;/b&gt;") || !strings.Contains(html, "/admin/listeners") {
		t.Errorf("user text not escaped in the HTML part (err %v)", err)
	}
	if !strings.HasPrefix(e.Subject, "Listen-along request from Eve") {
		t.Errorf("subject = %q", e.Subject)
	}
}
