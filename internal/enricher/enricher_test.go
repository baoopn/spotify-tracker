package enricher

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/soundcharts"
)

func TestFailureRequest(t *testing.T) {
	rl := &soundcharts.RetryableError{Status: 429, RetryAfter: 30 * time.Second}
	if r := failureRequest("t1", fmt.Errorf("lookup: %w", rl)); !r.GetRetryable() || r.GetRetryAfterSeconds() != 30 ||
		r.GetTrackId() != "t1" || r.GetError() == "" {
		t.Errorf("429: %+v", r)
	}
	if r := failureRequest("t1", &soundcharts.RetryableError{Status: 503, RetryAfter: time.Minute}); !r.GetRetryable() ||
		r.GetRetryAfterSeconds() != 60 {
		t.Errorf("503 with Retry-After: %+v", r)
	}
	for _, re := range []*soundcharts.RetryableError{{Status: 503}, {Status: 500}, {Status: 502, RetryAfter: time.Minute}, {Status: 0}} {
		if r := failureRequest("t1", re); r.GetRetryable() {
			t.Errorf("%d (retry-after %s) must count as an attempt: %+v", re.Status, re.RetryAfter, r)
		}
	}
	if r := failureRequest("t1", errors.New("decode song: bad json")); r.GetRetryable() || r.GetRetryAfterSeconds() != 0 {
		t.Errorf("permanent: %+v", r)
	}
}
