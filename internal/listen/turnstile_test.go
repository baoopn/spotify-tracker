package listen

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Cloudflare's documented test secrets.
const (
	passSecret = "1x0000000000000000000000000000000AA"
	failSecret = "2x0000000000000000000000000000000AA"
)

// fakeSiteverify answers like Cloudflare: the pass secret succeeds.
func fakeSiteverify(t *testing.T, hostname string, status int, got *[]map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = r.ParseForm()
		if got != nil {
			*got = append(*got, map[string]string{"secret": r.PostForm.Get("secret"),
				"response": r.PostForm.Get("response"), "remoteip": r.PostForm.Get("remoteip")})
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		ok := r.PostForm.Get("secret") == passSecret && r.PostForm.Get("response") != ""
		reply := map[string]any{"success": ok, "hostname": hostname, "error-codes": []string{}}
		if !ok {
			reply["error-codes"] = []string{"invalid-input-response"}
		}
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testTurnstile(srv *httptest.Server, secret, host string) *Turnstile {
	v := NewTurnstile(secret, host)
	v.url = srv.URL
	return v
}

func TestTurnstileVerify(t *testing.T) {
	var got []map[string]string
	srv := fakeSiteverify(t, "listening.example.com", http.StatusOK, &got)
	ctx := context.Background()

	if err := testTurnstile(srv, passSecret, "listening.example.com").Verify(ctx, "tok-1", "10.0.0.9"); err != nil {
		t.Errorf("pass = %v", err)
	}
	if len(got) != 1 || got[0]["response"] != "tok-1" || got[0]["remoteip"] != "10.0.0.9" || got[0]["secret"] != passSecret {
		t.Errorf("form = %v", got)
	}
	if err := testTurnstile(srv, failSecret, "listening.example.com").Verify(ctx, "tok", ""); !errors.Is(err, ErrTurnstileFailed) {
		t.Errorf("fail = %v", err)
	}
	// A token made for another site.
	if err := testTurnstile(srv, passSecret, "other.example.com").Verify(ctx, "tok", ""); !errors.Is(err, ErrTurnstileFailed) {
		t.Errorf("hostname mismatch = %v", err)
	}
	// Development: no hostname check.
	if err := testTurnstile(srv, passSecret, "").Verify(ctx, "tok", ""); err != nil {
		t.Errorf("no hostname check = %v", err)
	}
	if len(got) > 0 && got[len(got)-1]["remoteip"] != "" {
		t.Errorf("empty remoteip sent: %v", got[len(got)-1])
	}
}

func TestTurnstileUnavailable(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusBadRequest} {
		srv := fakeSiteverify(t, "", status, nil)
		err := testTurnstile(srv, passSecret, "").Verify(ctx, "tok", "")
		if !errors.Is(err, ErrTurnstileUnavailable) {
			t.Errorf("%d = %v", status, err)
		}
	}
	// Network error; the secret and token never show in the error.
	v := NewTurnstile(passSecret, "")
	v.url = "http://127.0.0.1:1/siteverify"
	err := v.Verify(ctx, "secret-token", "")
	if !errors.Is(err, ErrTurnstileUnavailable) || strings.Contains(err.Error(), passSecret) || strings.Contains(err.Error(), "secret-token") {
		t.Errorf("network = %v", err)
	}
	// Timeout.
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(300 * time.Millisecond) }))
	t.Cleanup(slow.Close)
	v = testTurnstile(slow, passSecret, "")
	v.client.Timeout = 50 * time.Millisecond
	if err := v.Verify(ctx, "tok", ""); !errors.Is(err, ErrTurnstileUnavailable) {
		t.Errorf("timeout = %v", err)
	}
	// A broken reply.
	junk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }))
	t.Cleanup(junk.Close)
	if err := testTurnstile(junk, passSecret, "").Verify(ctx, "tok", ""); !errors.Is(err, ErrTurnstileUnavailable) {
		t.Errorf("junk = %v", err)
	}
}

type fakeVerifier struct {
	err   error
	calls []string
}

func (f *fakeVerifier) Verify(_ context.Context, token, ip string) error {
	f.calls = append(f.calls, token+"@"+ip)
	return f.err
}

// The check runs after the rate limit and before anything is stored.
func TestRequestsTurnstileOrder(t *testing.T) {
	q, db, m, _, _ := newTestRequests()
	v := &fakeVerifier{err: ErrTurnstileFailed}
	q.WithTurnstile(v)
	ctx := context.Background()
	in := RequestInput{Name: "Ann", Email: "ann@example.com", ClientHash: "h", TurnstileToken: "tok", RemoteIP: "10.0.0.9"}
	if err := q.Submit(ctx, in); !errors.Is(err, ErrTurnstileFailed) {
		t.Fatalf("err = %v", err)
	}
	if len(db.pending) != 0 || len(m.sent) != 0 || len(v.calls) != 1 || v.calls[0] != "tok@10.0.0.9" {
		t.Errorf("stored %v, sent %d, calls %v", db.pending, len(m.sent), v.calls)
	}
	v.err = nil
	if err := q.Submit(ctx, in); err != nil || len(db.pending) != 1 {
		t.Errorf("pass = %v, stored %d", err, len(db.pending))
	}
	// Rate limited first: Cloudflare is not asked.
	calls := len(v.calls)
	for range RequestsPerHour {
		_ = q.Submit(ctx, in)
	}
	var limited ErrRateLimited
	if err := q.Submit(ctx, in); !errors.As(err, &limited) {
		t.Fatalf("limit = %v", err)
	}
	if len(v.calls) != calls+RequestsPerHour-2 {
		t.Errorf("verifier called %d times past the limit", len(v.calls)-calls)
	}
}
