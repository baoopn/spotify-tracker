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
