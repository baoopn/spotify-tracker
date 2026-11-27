package store

import (
	"errors"
	"testing"
	"time"
)

func TestListenThreshold(t *testing.T) {
	for dur, want := range map[int64]int64{0: 30000, -1: 30000, 40000: 20000, 60000: 30000, 200000: 30000} {
		if got := listenThreshold(dur); got != want {
			t.Errorf("threshold(%d) = %d, want %d", dur, got, want)
		}
	}
}

func TestListenCursor(t *testing.T) {
	at := time.Date(2026, 11, 26, 12, 0, 0, 123456000, time.UTC)
	gotAt, id, err := decodeListenCursor(EncodeListenCursor(at, 42))
	if err != nil || !gotAt.Equal(at) || id != 42 {
		t.Errorf("round trip = %v %d %v", gotAt, id, err)
	}
	// Not base64, id 0, "nope".
	for _, c := range []string{"%%", EncodeListenCursor(at, 0), "bm9wZQ"} {
		if _, _, err := decodeListenCursor(c); !errors.Is(err, ErrBadCursor) {
			t.Errorf("%q: err = %v", c, err)
		}
	}
}
