package notify

import (
	"errors"
	"strings"
	"testing"
)

func TestHTMLEscapesHostileInput(t *testing.T) {
	e := Email{
		Subject:    `<script>alert(1)</script>`,
		Preheader:  `"><script>p()</script>`,
		Tone:       Tone(`red;background:url(x)`),
		Badge:      `<b>badge</b>`,
		Title:      `Title "quoted" <script>t()</script>`,
		Paragraphs: []string{`<img src=x onerror=alert(1)>`},
		Facts:      []Fact{{`<i>label</i>`, `value' onmouseover='x`}},
		Code:       `</td><script>c()</script>`,
		Action:     &Action{Label: `<script>l()</script>`, URL: "javascript:alert(1)"},
		Note:       `<script>n()</script>`,
	}
	text, html, err := e.Render(testBrand(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script", "<img src=x", "<b>badge", "<i>label", "javascript:", "url(x)", "' onmouseover='"} {
		if strings.Contains(html, bad) {
			t.Errorf("HTML contains %q", bad)
		}
	}
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("escaped subject missing from <title>")
	}
	if !strings.Contains(html, "Title &#34;quoted&#34;") {
		t.Error("quotes in title not escaped")
	}
	if strings.Contains(html, "v:roundrect") || strings.Contains(html, `class="btn"`) {
		t.Error("button rendered for a javascript: URL")
	}
	if strings.Contains(text, "javascript:") {
		t.Error("text part contains the javascript: URL")
	}
	if !strings.Contains(html, `class="chip t-info"`) {
		t.Error("unknown tone did not fall back to info")
	}
}

func TestVMLButtonEscapes(t *testing.T) {
	got := string(vmlButton("https://example.com/a?b=1&c=2", `<"Label">`))
	if !strings.Contains(got, `href="https://example.com/a?b=1&amp;c=2"`) || !strings.Contains(got, "&lt;&#34;Label&#34;&gt;") {
		t.Errorf("vmlButton not escaped: %s", got)
	}
	if vmlButton("javascript:x", "L") != "" {
		t.Error("vmlButton kept a javascript: URL")
	}
}

func TestTextPartContent(t *testing.T) {
	text, _, err := sampleEmail().Render(testBrand(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[Expires in 7 days]",
		"Your Spotify connection expires soon",
		"Expires: 2027-05-04 00:00 UTC (in 7 days)",
		"Re-authorize Spotify:\n  https://listening.example.com/admin/settings/spotify",
		"Note text",
		"Sent by spotify-tracker · listening.example.com",
		"https://listening.example.com/admin/settings/email",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text part missing %q:\n%s", want, text)
		}
	}
}

func TestHTMLContent(t *testing.T) {
	e := sampleEmail()
	e.Code = strings.Repeat("x", 800)
	_, html, err := e.Render(testBrand(t))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(html); n > 30*1024 {
		t.Errorf("HTML is %d bytes; keep it under 30 KB (Gmail clips at 102 KB)", n)
	}
	for _, want := range []string{
		`src="https://listening.example.com/email/listening-wordmark-primary@2x.png"`,
		`alt="baoopn listening"`,
		`href="https://listening.example.com/admin/settings/spotify"`,
		`href="https://listening.example.com/admin/settings/email"`,
		"v:roundrect",
		`class="chip t-warning"`,
		"prefers-color-scheme: dark",
		"[data-ogsc]",
		strings.Repeat("x", maxCode) + "…",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if strings.Contains(html, strings.Repeat("x", maxCode+1)) {
		t.Error("code not truncated")
	}
}

func TestNewBrand(t *testing.T) {
	b, err := NewBrand("https://www.example.com/", "")
	if err != nil || b.URL != "https://www.example.com" || b.Host != "example.com" ||
		b.LogoURL != "https://www.example.com"+LogoPath || b.SettingsURL() != "https://www.example.com/admin/settings/email" {
		t.Errorf("default brand %+v, %v", b, err)
	}
	b, err = NewBrand("https://example.com", "https://cdn.example.com/logo.png")
	if err != nil || b.LogoURL != "https://cdn.example.com/logo.png" {
		t.Errorf("override %+v, %v", b, err)
	}
	for _, bad := range []string{"javascript:alert(1)", "/relative.png", "ftp://example.com/x.png", `https://example.com/"x`} {
		b, err = NewBrand("https://example.com", bad)
		if !errors.Is(err, ErrLogoURL) || b.LogoURL != "https://example.com"+LogoPath {
			t.Errorf("NewBrand(logo %q) = %+v, %v; want default + ErrLogoURL", bad, b, err)
		}
	}
	b, _ = NewBrand("not a url", "")
	if b.URL != "" || b.LogoURL != "" || b.SettingsURL() != "" {
		t.Errorf("invalid public URL gave %+v", b)
	}
	_, html, err := sampleEmail().Render(b)
	if err != nil || !strings.Contains(html, "baoopn listening") || strings.Contains(html, "<img") {
		t.Errorf("no-logo render: %v", err)
	}
}
