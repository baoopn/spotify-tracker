package notify

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"time"
)

func sampleEmail() Email {
	return Email{
		Subject:    "[spotify-tracker] Spotify token expires → soon — now",
		Preheader:  "Preheader text",
		Tone:       Warning,
		Badge:      "Expires in 7 days",
		Title:      "Your Spotify connection expires soon",
		Paragraphs: []string{"First paragraph. " + strings.Repeat("long line ", 200)},
		Facts:      []Fact{{"Expires", "2027-05-04 00:00 UTC (in 7 days)"}},
		Action:     &Action{Label: "Re-authorize Spotify", URL: "https://listening.example.com/admin/settings/spotify"},
		Note:       "Note text",
	}
}

func testBrand(t *testing.T) Brand {
	t.Helper()
	b, err := NewBrand("https://listening.example.com/", "")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// parts parses raw as multipart/alternative and returns each part's
// Content-Type and quoted-printable-decoded body.
func parts(t *testing.T, raw []byte) (*mail.Message, [][2]string) {
	t.Helper()
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" || params["boundary"] == "" {
		t.Fatalf("content type %q, %v", m.Header.Get("Content-Type"), err)
	}
	r := multipart.NewReader(m.Body, params["boundary"])
	var out [][2]string
	for {
		p, err := r.NextRawPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if cte := p.Header.Get("Content-Transfer-Encoding"); cte != "quoted-printable" {
			t.Errorf("transfer encoding %q", cte)
		}
		body, err := io.ReadAll(quotedprintable.NewReader(p))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, [2]string{p.Header.Get("Content-Type"), string(body)})
	}
	return m, out
}

func TestMessageStructure(t *testing.T) {
	e := sampleEmail()
	b := testBrand(t)
	raw, err := Message("Tracker <alerts@example.com>", []string{"a@example.com", "b@example.com"}, e, b, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(raw), "\r\n") {
		if len(l) > 998 {
			t.Fatalf("line %d is %d octets", i, len(l))
		}
	}
	m, ps := parts(t, raw)
	if len(ps) != 2 {
		t.Fatalf("got %d parts", len(ps))
	}
	if ps[0][0] != "text/plain; charset=utf-8" || ps[1][0] != "text/html; charset=utf-8" {
		t.Errorf("part types %q, %q", ps[0][0], ps[1][0])
	}
	text, html, err := e.Render(b)
	if err != nil {
		t.Fatal(err)
	}
	if ps[0][1] != strings.ReplaceAll(text, "\n", "\r\n") {
		t.Errorf("text part does not round-trip:\n%q", ps[0][1])
	}
	if ps[1][1] != strings.ReplaceAll(html, "\n", "\r\n") {
		t.Error("html part does not round-trip")
	}

	subj := m.Header.Get("Subject")
	if !strings.HasPrefix(subj, "=?utf-8?q?") {
		t.Errorf("non-ASCII subject not encoded: %q", subj)
	}
	dec, err := new(mime.WordDecoder).DecodeHeader(subj)
	if err != nil || dec != e.Subject {
		t.Errorf("subject decodes to %q, %v", dec, err)
	}
	if !regexp.MustCompile(`^<[0-9a-f]{32}@example\.com>$`).MatchString(m.Header.Get("Message-ID")) {
		t.Errorf("Message-ID %q", m.Header.Get("Message-ID"))
	}
	if m.Header.Get("MIME-Version") != "1.0" || m.Header.Get("Date") == "" {
		t.Error("missing MIME-Version or Date")
	}
	if got, err := m.Header.AddressList("To"); err != nil || len(got) != 2 {
		t.Errorf("To = %v, %v", got, err)
	}
}

func TestASCIISubjectUnencoded(t *testing.T) {
	raw, err := buildMessage("a@example.com", []string{"b@example.com"}, "[spotify-tracker] Test email", "t", "h", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("\r\nSubject: [spotify-tracker] Test email\r\n")) {
		t.Errorf("ASCII subject changed:\n%s", raw)
	}
}

func TestDomainOf(t *testing.T) {
	for in, want := range map[string]string{
		"a@example.com":             "example.com",
		"Name <alerts@example.org>": "example.org",
		"nobody":                    "localhost",
		"bad@<x>":                   "localhost",
	} {
		if got := domainOf(in); got != want {
			t.Errorf("domainOf(%q) = %q, want %q", in, got, want)
		}
	}
}
