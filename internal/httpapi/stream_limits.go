package httpapi

import (
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// Limits for GET /api/v1/now-playing/stream. Each open stream holds one Redis
// subscription, so the caps bound Redis connections; the lifetime makes
// clients reconnect now and then, which spreads them across API restarts.
const (
	maxStreamsPerIP = 6
	maxStreams      = 256
	maxStreamAge    = 30 * time.Minute
	streamAgeJitter = 5 * time.Minute
	// streamRetryAfter is the Retry-After (seconds) sent with a refused stream.
	streamRetryAfter = "30"
)

// streamSlots counts open streams, in total and per client IP bucket.
type streamSlots struct {
	mu    sync.Mutex
	total int
	perIP map[string]int
}

func newStreamSlots() *streamSlots {
	return &streamSlots{perIP: make(map[string]int)}
}

// acquire reserves a slot for ip. It returns status 0 and a release func on
// success, http.StatusTooManyRequests when ip already holds maxStreamsPerIP
// streams, or http.StatusServiceUnavailable when maxStreams are open. release
// is safe to call more than once; only the first call frees the slot.
func (s *streamSlots) acquire(ip string) (release func(), status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perIP[ip] >= maxStreamsPerIP {
		return nil, http.StatusTooManyRequests
	}
	if s.total >= maxStreams {
		return nil, http.StatusServiceUnavailable
	}
	s.perIP[ip]++
	s.total++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.total--
			if n := s.perIP[ip] - 1; n > 0 {
				s.perIP[ip] = n
			} else {
				delete(s.perIP, ip)
			}
		})
	}, 0
}

// randomStreamAge picks a lifetime in [maxStreamAge-jitter, maxStreamAge+jitter]
// so streams opened together do not all reconnect at the same moment.
func randomStreamAge() time.Duration {
	return maxStreamAge - streamAgeJitter + rand.N(2*streamAgeJitter+1) //nolint:gosec // not security sensitive
}

func (h *Handler) streamLifetime() time.Duration {
	if h.streamAge != nil {
		return h.streamAge()
	}
	return randomStreamAge()
}

// refuseStream answers a stream request that found no free slot.
func refuseStream(w http.ResponseWriter, status int) {
	w.Header().Set("Retry-After", streamRetryAfter)
	if status == http.StatusTooManyRequests {
		writeError(w, status, "too_many_streams", "too many open streams from this client")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "stream_busy", "too many open streams; retry later")
}
