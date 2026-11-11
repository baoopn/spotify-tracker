package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStreamSlotsPerIPAndTotal(t *testing.T) {
	s := newStreamSlots()
	var releases []func()
	for i := range maxStreamsPerIP {
		rel, st := s.acquire("a")
		if st != 0 {
			t.Fatalf("acquire %d for a: status %d", i, st)
		}
		releases = append(releases, rel)
	}
	if _, st := s.acquire("a"); st != http.StatusTooManyRequests {
		t.Fatalf("over per-IP cap: status %d, want 429", st)
	}
	// Other IPs fill the rest of the total.
	for i := maxStreamsPerIP; i < maxStreams; i++ {
		if _, st := s.acquire("ip-" + strconv.Itoa(i)); st != 0 {
			t.Fatalf("acquire %d: status %d", i, st)
		}
	}
	if _, st := s.acquire("fresh"); st != http.StatusServiceUnavailable {
		t.Fatalf("over total cap: status %d, want 503", st)
	}
	// A full client still gets 429, not 503.
	if _, st := s.acquire("a"); st != http.StatusTooManyRequests {
		t.Fatalf("full client at total cap: status %d, want 429", st)
	}
	releases[0]()
	if _, st := s.acquire("fresh"); st != 0 {
		t.Fatalf("after release: status %d, want 0", st)
	}
}

func TestStreamSlotsReleaseIdempotent(t *testing.T) {
	s := newStreamSlots()
	rel1, _ := s.acquire("a")
	rel2, _ := s.acquire("a")
	rel1()
	rel1()
	rel1()
	if s.total != 1 || s.perIP["a"] != 1 {
		t.Fatalf("after repeated release: total=%d perIP=%d, want 1/1", s.total, s.perIP["a"])
	}
	rel2()
	rel2()
	if s.total != 0 {
		t.Fatalf("total = %d, want 0", s.total)
	}
	if _, ok := s.perIP["a"]; ok || len(s.perIP) != 0 {
		t.Fatalf("zero entries not deleted: %v", s.perIP)
	}
}

// streamHandler has a nil rdb: a request that reaches Redis would panic, so a
// clean refusal proves the cap is checked first.
func streamHandler() *Handler {
	return &Handler{log: slog.New(slog.DiscardHandler), streams: newStreamSlots()}
}

func checkRefused(t *testing.T, rec *httptest.ResponseRecorder, code int, errCode string) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status = %d, want %d", rec.Code, code)
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want 30", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if rec.Header().Get("X-Accel-Buffering") != "" {
		t.Error("SSE headers sent on a refused stream")
	}
	if !strings.Contains(rec.Body.String(), `"code":"`+errCode+`"`) {
		t.Errorf("body = %s, want code %s", rec.Body, errCode)
	}
}

func TestStreamOverPerIPCap429(t *testing.T) {
	h := streamHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/now-playing/stream", nil)
	ip, _ := clientIPKey(req)
	for range maxStreamsPerIP {
		if _, st := h.streams.acquire(ip); st != 0 {
			t.Fatalf("prefill: status %d", st)
		}
	}
	rec := httptest.NewRecorder()
	h.nowPlayingStream(rec, req)
	checkRefused(t, rec, http.StatusTooManyRequests, "too_many_streams")
	if h.streams.total != maxStreamsPerIP {
		t.Errorf("refused request changed the count: %d", h.streams.total)
	}
}

func TestStreamOverTotalCap503(t *testing.T) {
	h := streamHandler()
	for i := range maxStreams {
		if _, st := h.streams.acquire("other-" + strconv.Itoa(i)); st != 0 {
			t.Fatalf("prefill %d: status %d", i, st)
		}
	}
	rec := httptest.NewRecorder()
	h.nowPlayingStream(rec, httptest.NewRequest(http.MethodGet, "/api/v1/now-playing/stream", nil))
	checkRefused(t, rec, http.StatusServiceUnavailable, "stream_busy")
}

func TestStreamAgeWithinJitter(t *testing.T) {
	lo, hi := maxStreamAge-streamAgeJitter, maxStreamAge+streamAgeJitter
	seen := map[time.Duration]bool{}
	for range 1000 {
		d := randomStreamAge()
		if d < lo || d > hi {
			t.Fatalf("age %v outside [%v, %v]", d, lo, hi)
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Error("no jitter: every age was the same")
	}
	h := &Handler{streamAge: func() time.Duration { return time.Second }}
	if got := h.streamLifetime(); got != time.Second {
		t.Errorf("streamLifetime seam = %v", got)
	}
}
