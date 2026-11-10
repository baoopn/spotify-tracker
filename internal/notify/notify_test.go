package notify

import (
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildMessageStripsHeaderInjection(t *testing.T) {
	raw, err := buildMessage("a@example.com", []string{"b@example.com\r\nCc: evil@example.com"},
		"hello\r\nBcc: evil@example.com", "text", "<p>html</p>", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	msg := string(raw)
	if strings.Contains(msg, "\r\nBcc:") || strings.Contains(msg, "\r\nCc:") {
		t.Fatalf("header injection not prevented:\n%s", msg)
	}
	m, err := mail.ReadMessage(strings.NewReader(msg))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Header.Get("Subject"); got != "hello Bcc: evil@example.com" {
		t.Errorf("subject not sanitised: %q", got)
	}
	if m.Header.Get("Bcc") != "" || m.Header.Get("Cc") != "" {
		t.Error("injected header parsed")
	}
}

func TestParseRecipients(t *testing.T) {
	got, err := ParseRecipients(" a@x.com, ,b@y.org ")
	if err != nil || len(got) != 2 || got[1] != "b@y.org" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := ParseRecipients("nope"); err == nil {
		t.Fatal("expected error")
	}
	if got, _ := ParseRecipients(""); len(got) != 0 {
		t.Fatal("expected empty")
	}
}

func TestLogoHeightFor(t *testing.T) {
	for _, c := range []struct{ w, h, want int }{
		{640, 264, 66}, {320, 132, 66}, {0, 0, 66}, {200, 200, 160}, {10, 1000, 66},
		{-1, 5, 66}, {640, 0, 66}, {254, 121, 76}, {1600, 100, 66},
	} {
		if got := LogoHeightFor(c.w, c.h); got != c.want {
			t.Errorf("LogoHeightFor(%d, %d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
}

func TestWithLogo(t *testing.T) {
	b := testBrand(t)
	got := b.WithLogo("https://media.baoopn.com/brand/listening/email-logo.png", 200, 200)
	if got.LogoURL != "https://media.baoopn.com/brand/listening/email-logo.png" || got.LogoHeight != 160 {
		t.Errorf("WithLogo = %+v", got)
	}
	for _, bad := range []string{"", "javascript:alert(1)", "/rel.png", `https://x.com/"a`, "https://u:p@x.com/a.png"} {
		if got := b.WithLogo(bad, 200, 200); got != b {
			t.Errorf("WithLogo(%q) changed the brand: %+v", bad, got)
		}
	}
}

func TestNotifierLogoOrder(t *testing.T) {
	const cached = "https://media.baoopn.com/brand/listening/email-logo.abc.png"
	static := testBrand(t)
	override, err := NewBrand("https://listening.example.com/", "https://cdn.example.com/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	var have bool
	logo := func() (string, int, int, bool) { return cached, 640, 264, have }

	for _, c := range []struct {
		name  string
		brand Brand
		have  bool
		want  string
	}{
		{"cache wins over override", override, true, cached},
		{"cache wins over static", static, true, cached},
		{"override when cache empty", override, false, "https://cdn.example.com/logo.png"},
		{"static when both empty", static, false, "https://listening.example.com" + LogoPath},
	} {
		n := New(nil, nil, c.brand, nil)
		n.SetLogo(logo)
		have = c.have
		if got := n.Brand().LogoURL; got != c.want {
			t.Errorf("%s: logo = %q, want %q", c.name, got, c.want)
		}
	}
	n := New(nil, nil, static, nil)
	if n.Brand() != static {
		t.Error("no LogoFunc should keep the static brand")
	}
	n.SetLogo(nil)
	if n.Brand() != static {
		t.Error("nil LogoFunc should keep the static brand")
	}
}

func TestRenderedLogoHeight(t *testing.T) {
	b := testBrand(t).WithLogo("https://media.baoopn.com/brand/listening/square.png", 200, 200)
	_, html, err := sampleEmail().Render(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`src="https://media.baoopn.com/brand/listening/square.png"`, `width="160" height="160"`, "height:160px"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	_, html, _ = sampleEmail().Render(testBrand(t))
	if !strings.Contains(html, `width="160" height="66"`) {
		t.Error("default logo height is not 66")
	}
}
