package notify

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"slices"
	"strings"
	"sync"
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

// fakeSMTP is a minimal SMTP server. It offers STARTTLS before TLS and, when
// advertiseAuth is set, AUTH PLAIN after it. It records the commands it got.
type fakeSMTP struct {
	ln            net.Listener
	tlsConfig     *tls.Config
	advertiseAuth bool

	mu   sync.Mutex
	cmds []string
	data string
	done chan struct{}
}

func newFakeSMTP(t *testing.T, advertiseAuth bool) *fakeSMTP {
	t.Helper()
	// Borrow httptest's certificate (valid for 127.0.0.1) and trust it.
	hs := httptest.NewTLSServer(http.NotFoundHandler())
	certs := hs.TLS.Certificates
	pool := x509.NewCertPool()
	pool.AddCert(hs.Certificate())
	hs.Close()
	old := smtpRootCAs
	smtpRootCAs = pool
	t.Cleanup(func() { smtpRootCAs = old })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, tlsConfig: &tls.Config{Certificates: certs}, advertiseAuth: advertiseAuth, done: make(chan struct{})}
	t.Cleanup(func() { _ = ln.Close() })
	go f.serve()
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.cmds)
}

func (f *fakeSMTP) serve() {
	defer close(f.done)
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	tp := textproto.NewConn(conn)
	secure := false
	_ = tp.PrintfLine("220 fake ESMTP")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb := strings.ToUpper(strings.Fields(line + " x")[0])
		f.mu.Lock()
		f.cmds = append(f.cmds, verb)
		f.mu.Unlock()
		switch verb {
		case "EHLO":
			switch {
			case !secure:
				_ = tp.PrintfLine("250-fake\r\n250 STARTTLS")
			case f.advertiseAuth:
				_ = tp.PrintfLine("250-fake\r\n250 AUTH PLAIN")
			default:
				_ = tp.PrintfLine("250 fake")
			}
		case "STARTTLS":
			_ = tp.PrintfLine("220 go ahead")
			tc := tls.Server(conn, f.tlsConfig)
			if tc.Handshake() != nil {
				return
			}
			conn, secure = tc, true
			tp = textproto.NewConn(tc)
		case "AUTH":
			_ = tp.PrintfLine("235 ok")
		case "DATA":
			_ = tp.PrintfLine("354 go")
			b, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.data = string(b)
			f.mu.Unlock()
			_ = tp.PrintfLine("250 queued")
		case "QUIT":
			_ = tp.PrintfLine("221 bye")
			return
		default:
			_ = tp.PrintfLine("250 ok")
		}
	}
}

func TestDeliverStartTLSAndAuth(t *testing.T) {
	f := newFakeSMTP(t, true)
	cfg := SMTPConfig{Host: "127.0.0.1", Port: f.port(), User: "u", Password: "p", From: "a@example.com", To: []string{"b@example.com"}}
	if err := deliver(cfg, []byte("Subject: hi\r\n\r\nbody\r\n")); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	<-f.done
	want := []string{"EHLO", "STARTTLS", "EHLO", "AUTH", "MAIL", "RCPT", "DATA", "QUIT"}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
	if !strings.Contains(f.data, "body") {
		t.Errorf("message not delivered: %q", f.data)
	}
}

func TestDeliverAuthRequiredButNotAdvertised(t *testing.T) {
	f := newFakeSMTP(t, false)
	cfg := SMTPConfig{Host: "127.0.0.1", Port: f.port(), User: "u", Password: "p", From: "a@example.com", To: []string{"b@example.com"}}
	err := deliver(cfg, []byte("x\r\n"))
	if err == nil || !strings.Contains(err.Error(), "AUTH") {
		t.Fatalf("err = %v, want an AUTH error", err)
	}
	if slices.Contains(f.commands(), "MAIL") {
		t.Error("sent MAIL without authenticating")
	}
}

func TestDeliverWithoutAuth(t *testing.T) {
	f := newFakeSMTP(t, false)
	cfg := SMTPConfig{Host: "127.0.0.1", Port: f.port(), From: "a@example.com", To: []string{"b@example.com"}}
	if err := deliver(cfg, []byte("x\r\n")); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	<-f.done
	if slices.Contains(f.commands(), "AUTH") {
		t.Error("authenticated without credentials")
	}
}

func TestDeliverDeadline(t *testing.T) {
	old := smtpTimeout
	smtpTimeout = 300 * time.Millisecond
	t.Cleanup(func() { smtpTimeout = old })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		// Accept and say nothing until the client hangs up.
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = io.Copy(io.Discard, c)
	}()

	cfg := SMTPConfig{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, From: "a@example.com", To: []string{"b@example.com"}}
	start := time.Now()
	errc := make(chan error, 1)
	go func() { errc <- deliver(cfg, []byte("x\r\n")) }()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("deliver to a silent server succeeded")
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("deliver took %s, want about %s", d, smtpTimeout)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deliver did not return from a silent server")
	}
}
