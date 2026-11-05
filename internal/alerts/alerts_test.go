package alerts

import (
	"testing"
	"time"
)

func TestStage(t *testing.T) {
	day := 24 * time.Hour
	stages := []int{14, 7, 3, 1}
	tests := []struct {
		left   time.Duration
		want   int
		wantOK bool
	}{
		{30 * day, 0, false},
		{14*day + time.Hour, 0, false},
		{14 * day, 14, true},
		{10 * day, 14, true},
		{5 * day, 7, true},
		{2 * day, 3, true},
		{12 * time.Hour, 1, true},
		{0, 0, true},
		{-3 * day, 0, true},
	}
	for _, tt := range tests {
		got, ok := Stage(tt.left, stages)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("Stage(%s) = %d,%v; want %d,%v", tt.left, got, ok, tt.want, tt.wantOK)
		}
	}
	if _, ok := Stage(5*day, []int{3}); ok {
		t.Error("custom stages ignored")
	}
}

func TestExpiresAt(t *testing.T) {
	at := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	if got := ExpiresAt(at, 180); !got.Equal(time.Date(2027, 5, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ExpiresAt = %v", got)
	}
}
