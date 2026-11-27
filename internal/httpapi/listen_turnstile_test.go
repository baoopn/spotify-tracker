package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/listen"
)

// Cloudflare's documented always-pass test site key.
const testSiteKey = "1x00000000000000000000AA"

func TestListenConfigTurnstileKey(t *testing.T) {
	for _, c := range []struct {
		o    *ListenOptions
		want string
	}{
		{&ListenOptions{Enabled: true, TurnstileSiteKey: testSiteKey}, `"turnstile_site_key":"` + testSiteKey + `"`},
		{&ListenOptions{Enabled: true}, `"turnstile_site_key":null`},
		{&ListenOptions{Enabled: false, TurnstileSiteKey: testSiteKey}, `"turnstile_site_key":null`},
	} {
		if rec, out := get(t, listenRouter(c.o, nil), "/api/v1/listen/config"); rec.Code != 200 || !strings.Contains(string(out["data"]), c.want) {
			t.Errorf("config = %s, want %s", out["data"], c.want)
		}
	}
}

func TestListenRequestTurnstile(t *testing.T) {
	const body = `{"name":"Ann","email":"ann@example.com","turnstile_token":" tok-1 "}`
	sub := &fakeSubmitter{}
	on := listenRouter(&ListenOptions{Enabled: true, Requests: sub, TurnstileSiteKey: testSiteKey}, nil)

	if rec := post(on, "/api/v1/listen/requests", body); rec.Code != 202 || len(sub.got) != 1 ||
		sub.got[0].TurnstileToken != "tok-1" || sub.got[0].RemoteIP != strings.Split(httptest.NewRequest(http.MethodGet, "/", nil).RemoteAddr, ":")[0] {
		t.Fatalf("pass = %d %s %+v", rec.Code, rec.Body, sub.got)
	}
	// Missing or empty token: 422 before anything is submitted.
	for _, b := range []string{`{"name":"Ann","email":"ann@example.com"}`, `{"name":"Ann","email":"ann@example.com","turnstile_token":"  "}`} {
		rec := post(on, "/api/v1/listen/requests", b)
		if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"turnstile":"Complete the check"`) {
			t.Errorf("missing = %d %s", rec.Code, rec.Body)
		}
	}
	if len(sub.got) != 1 {
		t.Errorf("missing token submitted: %d", len(sub.got))
	}
	sub.err = listen.ErrTurnstileFailed
	if rec := post(on, "/api/v1/listen/requests", body); rec.Code != 422 ||
		!strings.Contains(rec.Body.String(), `"turnstile":"The check failed. Try again."`) {
		t.Errorf("failed = %d %s", rec.Code, rec.Body)
	}
	sub.err = listen.ErrTurnstileUnavailable
	rec := post(on, "/api/v1/listen/requests", body)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"turnstile_unavailable"`) ||
		strings.Contains(rec.Body.String(), "tok-1") {
		t.Errorf("unavailable = %d %s", rec.Code, rec.Body)
	}
	// The honeypot still answers 202 without a check.
	sub.err, sub.got = nil, nil
	if rec := post(on, "/api/v1/listen/requests", `{"name":"Bot","email":"b@example.com","website":"x"}`); rec.Code != 202 || len(sub.got) != 0 {
		t.Errorf("honeypot = %d, submitted %d", rec.Code, len(sub.got))
	}

	// Turnstile off: no token needed.
	off := listenRouter(&ListenOptions{Enabled: true, Requests: sub}, nil)
	if rec := post(off, "/api/v1/listen/requests", `{"name":"Ann","email":"ann@example.com"}`); rec.Code != 202 || len(sub.got) != 1 {
		t.Errorf("off = %d %s", rec.Code, rec.Body)
	}
}
