package tracker

import (
	"testing"
	"time"
)

func ptr(v int32) *int32 { return &v }

func TestDecide(t *testing.T) {
	const threshold = 10 * time.Second
	tests := []struct {
		name     string
		prev     State
		track    string
		progress *int32
		want     Decision
	}{
		{"first ever snapshot", State{}, "a", ptr(1000), Decision{NewHistory: true}},
		{"track changed", State{TrackID: "a", ProgressMS: ptr(90_000)}, "b", ptr(500), Decision{NewHistory: true}},
		{"same track progressing", State{TrackID: "a", ProgressMS: ptr(10_000)}, "a", ptr(13_000), Decision{}},
		{"small seek back is not a replay", State{TrackID: "a", ProgressMS: ptr(30_000)}, "a", ptr(25_000), Decision{}},
		{"restart from beginning is a replay", State{TrackID: "a", ProgressMS: ptr(180_000)}, "a", ptr(2_000), Decision{Replay: true}},
		{"unknown previous progress", State{TrackID: "a"}, "a", ptr(2_000), Decision{}},
		{"unknown current progress", State{TrackID: "a", ProgressMS: ptr(180_000)}, "a", nil, Decision{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.prev, tt.track, tt.progress, threshold); got != tt.want {
				t.Fatalf("Decide() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
