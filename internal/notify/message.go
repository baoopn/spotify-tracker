package notify

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Message renders e and wraps it in a complete RFC 5322 message:
// multipart/alternative with a text/plain part first and a text/html part
// second, both UTF-8 and quoted-printable (so no line exceeds 998 octets).
func Message(from string, to []string, e Email, b Brand, now time.Time) ([]byte, error) {
	text, html, err := e.Render(b)
	if err != nil {
		return nil, err
	}
	return buildMessage(from, to, e.Subject, text, html, now)
}

// headerValue keeps a header value on one line: CR and LF cannot start a
// new header.
func headerValue(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

func buildMessage(from string, to []string, subject, text, html string, now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	header("From", headerValue(from))
	header("To", headerValue(strings.Join(to, ", ")))
	// QEncoding leaves plain ASCII unchanged and encodes "→", "—" etc.
	header("Subject", mime.QEncoding.Encode("utf-8", headerValue(subject)))
	header("Date", now.UTC().Format(time.RFC1123Z))
	header("Message-ID", "<"+randomID()+"@"+domainOf(from)+">")
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	buf.WriteString("\r\n")

	for _, part := range []struct{ ctype, body string }{
		{"text/plain; charset=utf-8", text},
		{"text/html; charset=utf-8", html},
	} {
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// domainOf is the domain of the sender address (From may be "Name <a@b>").
func domainOf(from string) string {
	addr := from
	if a, err := mail.ParseAddress(from); err == nil {
		addr = a.Address
	}
	if _, d, ok := strings.Cut(addr, "@"); ok {
		d = strings.TrimSpace(d)
		if d != "" && !strings.ContainsAny(d, " <>\"\r\n@") {
			return d
		}
	}
	return "localhost"
}
