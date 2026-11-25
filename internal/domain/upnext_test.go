package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// The public now-playing body (also the MCP now_playing tool) must never
// carry the private queue.
func TestNowPlayingHasNoQueue(t *testing.T) {
	b, err := json.Marshal(NowPlaying{Track: &TrackView{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"up_next", "queue", "upcoming"} {
		if strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("now playing JSON has %q: %s", k, b)
		}
	}
}
