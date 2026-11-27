package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/listen"
)

type fakeSubmitter struct {
	got []listen.RequestInput
	err error
}

func (f *fakeSubmitter) Submit(_ context.Context, in listen.RequestInput) error {
	f.got = append(f.got, in)
	return f.err
}

func TestListenRequestEndpoint(t *testing.T) {
	sub := &fakeSubmitter{}
	// No Auth: the route is public.
	o := &ListenOptions{Enabled: true, Requests: sub,
		ClientHash: func(ip string) string { return analytics.HashIP("salt", "listen-request", ip) }}
	h := listenRouter(o, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/listen/requests",
		strings.NewReader(`{"name":"  Ann\u0000 ","email":" Ann@Example.com ","message":"hi\r\nthere"}`))
	req.Header.Set("CF-IPCountry", "VN")
	req.RemoteAddr = "203.0.113.9:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 202 || strings.TrimSpace(rec.Body.String()) != `{"ok":true}` || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("request = %d %s", rec.Code, rec.Body)
	}
	in := sub.got[0]
	if in.Name != "Ann" || in.Email != "ann@example.com" || in.Message != "hi\nthere" || in.Country != "VN" ||
		len(in.ClientHash) != 32 || strings.Contains(in.ClientHash, "203.0.113") {
		t.Errorf("input = %+v", in)
	}

	// Honeypot: 202, nothing submitted.
	if rec := post(h, "/api/v1/listen/requests", `{"name":"Bot","email":"bot@example.com","website":"http://spam.example"}`); rec.Code != 202 || len(sub.got) != 1 {
		t.Errorf("honeypot = %d, submitted %d", rec.Code, len(sub.got))
	}
	for _, c := range []struct{ body, field string }{
		{`{"name":"","email":"a@example.com"}`, "name"},
		{`{"name":"` + strings.Repeat("é", 81) + `","email":"a@example.com"}`, "name"},
		{`{"name":"A","email":"A <a@example.com>"}`, "email"},
		{`{"name":"A","email":"a@example.com","message":"` + strings.Repeat("m", 501) + `"}`, "message"},
	} {
		rec := post(h, "/api/v1/listen/requests", c.body)
		if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"`+c.field+`"`) {
			t.Errorf("%.40s = %d %s", c.body, rec.Code, rec.Body)
		}
	}
	if rec := post(h, "/api/v1/listen/requests", `{"name":"A","email":"a@example.com","admin":true}`); rec.Code != 400 {
		t.Errorf("unknown field = %d", rec.Code)
	}
	if len(sub.got) != 1 {
		t.Errorf("invalid input submitted: %d", len(sub.got))
	}

	sub.err = listen.ErrRateLimited{RetryAfter: 90 * time.Second}
	rec = post(h, "/api/v1/listen/requests", `{"name":"A","email":"a@example.com"}`)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "90" || !strings.Contains(rec.Body.String(), "rate_limited") {
		t.Errorf("limited = %d %v %s", rec.Code, rec.Header(), rec.Body)
	}

	off := listenRouter(&ListenOptions{Enabled: false, Requests: sub}, nil)
	if rec := post(off, "/api/v1/listen/requests", `{}`); rec.Code != 404 || !strings.Contains(rec.Body.String(), "listen_disabled") {
		t.Errorf("disabled = %d %s", rec.Code, rec.Body)
	}
}
