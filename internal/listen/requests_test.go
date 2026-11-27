package listen

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/notify"
	"github.com/baoopn/spotify-tracker/internal/store"
)

type memRate struct{ m map[string]int64 }

func (r *memRate) incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	r.m[key]++
	return r.m[key], nil
}

type fakeRequestStore struct {
	pending map[string]store.ListenRequest
	next    int64
	full    bool
	audits  []string
}

func (f *fakeRequestStore) SubmitListenRequest(_ context.Context, n store.NewListenRequest, max int) (store.ListenRequest, bool, error) {
	if r, ok := f.pending[n.Email]; ok {
		r.Name = n.Name
		f.pending[n.Email] = r
		return r, false, nil
	}
	if f.full || len(f.pending) >= max {
		return store.ListenRequest{}, false, store.ErrRequestsFull
	}
	f.next++
	r := store.ListenRequest{ID: f.next, Name: n.Name, Email: n.Email, Status: store.RequestPending}
	f.pending[n.Email] = r
	return r, true, nil
}

func (f *fakeRequestStore) Audit(_ context.Context, actor, action string, _ any, _ string) error {
	f.audits = append(f.audits, actor+" "+action)
	return nil
}

type fakeMailer struct {
	configured bool
	sent       []notify.Email
	err        error
}

func (m *fakeMailer) Configured(context.Context) bool { return m.configured }
func (m *fakeMailer) Send(_ context.Context, _ []string, e notify.Email) error {
	m.sent = append(m.sent, e)
	return m.err
}

func newTestRequests() (*Requests, *fakeRequestStore, *fakeMailer, *memRate, *bytes.Buffer) {
	db := &fakeRequestStore{pending: map[string]store.ListenRequest{}}
	m := &fakeMailer{configured: true}
	rate := &memRate{m: map[string]int64{}}
	var logs bytes.Buffer
	now := time.Date(2026, 11, 26, 12, 40, 0, 0, time.UTC)
	q := &Requests{db: db, rate: rate, mail: m, log: slog.New(slog.NewTextHandler(&logs, nil)),
		now:    func() time.Time { return now },
		goFunc: func(f func()) { f() },
		email: func(r store.ListenRequest) notify.Email {
			return notify.Email{Subject: "Listen-along request from " + r.Name}
		}}
	return q, db, m, rate, &logs
}

func TestRequestsAlertOnce(t *testing.T) {
	q, db, m, _, _ := newTestRequests()
	ctx := context.Background()
	in := RequestInput{Name: "Ann", Email: "ann@example.com", ClientHash: "h1"}
	if err := q.Submit(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.Name = "Ann B"
	if err := q.Submit(ctx, in); err != nil {
		t.Fatal(err)
	}
	if len(m.sent) != 1 || m.sent[0].Subject != "Listen-along request from Ann" {
		t.Errorf("sent = %+v", m.sent)
	}
	if db.pending["ann@example.com"].Name != "Ann B" || len(db.audits) != 1 || db.audits[0] != "ann@example.com listener.requested" {
		t.Errorf("store = %+v audits %v", db.pending, db.audits)
	}
	// No SMTP: no email, no error.
	m.configured = false
	if err := q.Submit(ctx, RequestInput{Name: "Bob", Email: "bob@example.com", ClientHash: "h2"}); err != nil || len(m.sent) != 1 {
		t.Errorf("unconfigured = %v, sent %d", err, len(m.sent))
	}
	// A failing mailer never fails the request.
	m.configured, m.err = true, errors.New("smtp down")
	if err := q.Submit(ctx, RequestInput{Name: "Cy", Email: "cy@example.com", ClientHash: "h3"}); err != nil {
		t.Errorf("mailer error leaked: %v", err)
	}
}

func TestRequestsRateLimit(t *testing.T) {
	q, _, _, rate, _ := newTestRequests()
	ctx := context.Background()
	for i := range RequestsPerHour {
		if err := q.Submit(ctx, RequestInput{Name: "x", Email: "x@example.com", ClientHash: "same"}); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	err := q.Submit(ctx, RequestInput{Name: "x", Email: "y@example.com", ClientHash: "same"})
	var limited ErrRateLimited
	if !errors.As(err, &limited) || limited.RetryAfter != 20*time.Minute {
		t.Errorf("err = %v (%v)", err, limited.RetryAfter)
	}
	// Another client is not limited; keys hold only the hash.
	if err := q.Submit(ctx, RequestInput{Name: "x", Email: "z@example.com", ClientHash: "other"}); err != nil {
		t.Errorf("other client = %v", err)
	}
	for k := range rate.m {
		if !strings.HasPrefix(k, "spotify-tracker:listen:requests:rate:") || strings.Contains(k, ".") {
			t.Errorf("rate key = %q", k)
		}
	}
}

func TestRequestsPendingCap(t *testing.T) {
	q, db, m, _, logs := newTestRequests()
	db.full = true
	if err := q.Submit(context.Background(), RequestInput{Name: "x", Email: "x@example.com", ClientHash: "h"}); err != nil {
		t.Errorf("full = %v (must look like success)", err)
	}
	if len(m.sent) != 0 || !strings.Contains(logs.String(), "too many pending") {
		t.Errorf("sent %d, logs %s", len(m.sent), logs)
	}
}

func TestCleanTextAndEmail(t *testing.T) {
	zwsp := string(rune(0x200b))
	if got := CleanText("  Ann\x00"+zwsp+"\r\nBcc: x\t ", false); got != "Ann Bcc: x" {
		t.Errorf("name = %q", got)
	}
	if got := CleanText("line 1\r\nline 2\x07", true); got != "line 1\nline 2" {
		t.Errorf("message = %q", got)
	}
	for in, ok := range map[string]bool{" Ann@Example.com ": true, "Ann <a@x.io>": false, "nope": false, "a@b@c": false} {
		if _, got := NormalizeEmail(in); got != ok {
			t.Errorf("NormalizeEmail(%q) = %v", in, got)
		}
	}
}
