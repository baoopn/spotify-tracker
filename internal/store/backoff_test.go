package store

import (
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	cases := map[int]time.Duration{
		0:  time.Minute,
		1:  time.Minute,
		2:  2 * time.Minute,
		3:  4 * time.Minute,
		8:  128 * time.Minute,
		12: 24 * time.Hour,
		99: 24 * time.Hour,
	}
	for n, want := range cases {
		if got := Backoff(n); got != want {
			t.Errorf("Backoff(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestSortedSubGenres(t *testing.T) {
	got := sortedSubGenres(map[string]int64{"indie": 3, "alt": 3, "shoegaze": 9})
	if len(got) != 3 || got[0].Genre != "shoegaze" || got[1].Genre != "alt" || got[2].Genre != "indie" {
		t.Errorf("got %+v", got)
	}
	if out := sortedSubGenres(nil); out == nil || len(out) != 0 {
		t.Errorf("nil map should give empty slice, got %#v", out)
	}
}

func TestPostponeDelay(t *testing.T) {
	for _, c := range []struct {
		attempts   int
		retryAfter time.Duration
		want       time.Duration
	}{
		{0, 0, time.Minute},                     // normal backoff, no attempt counted
		{3, 0, 4 * time.Minute},                 // backoff for the attempts so far
		{3, 90 * time.Second, 90 * time.Second}, // Retry-After wins
		{0, 48 * time.Hour, 24 * time.Hour},     // capped
	} {
		if got := PostponeDelay(c.attempts, c.retryAfter); got != c.want {
			t.Errorf("PostponeDelay(%d, %s) = %s, want %s", c.attempts, c.retryAfter, got, c.want)
		}
	}
}
