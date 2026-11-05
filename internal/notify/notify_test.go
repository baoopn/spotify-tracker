package notify

import (
	"strings"
	"testing"
	"time"
)

func TestBuildMessageStripsHeaderInjection(t *testing.T) {
	msg := string(buildMessage("a@example.com", []string{"b@example.com"},
		"hello\r\nBcc: evil@example.com", "line1\nline2", time.Unix(0, 0)))
	if strings.Contains(msg, "\r\nBcc:") {
		t.Fatalf("header injection not prevented:\n%s", msg)
	}
	if !strings.Contains(msg, "Subject: hello Bcc: evil@example.com\r\n") {
		t.Errorf("subject not sanitised:\n%s", msg)
	}
	if !strings.HasSuffix(msg, "line1\r\nline2\r\n") {
		t.Errorf("body not CRLF-normalised: %q", msg)
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
