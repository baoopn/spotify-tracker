package domain

import (
	"testing"
	"time"
)

func TestParseRange(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in       string
		wantName string
		wantDays int
		ok       bool
	}{
		{"", "30d", 30, true},
		{"7d", "7d", 7, true},
		{"365d", "365d", 365, true},
		{"all", "all", -1, true},
		{"2w", "", 0, false},
	}
	for _, tt := range tests {
		r, ok := ParseRange(tt.in, now)
		if ok != tt.ok {
			t.Fatalf("ParseRange(%q) ok = %v, want %v", tt.in, ok, tt.ok)
		}
		if !ok {
			continue
		}
		if r.Name != tt.wantName {
			t.Errorf("ParseRange(%q).Name = %q, want %q", tt.in, r.Name, tt.wantName)
		}
		if tt.wantDays < 0 {
			if !r.Since.IsZero() {
				t.Errorf("ParseRange(all).Since = %v, want zero", r.Since)
			}
		} else if want := now.AddDate(0, 0, -tt.wantDays); !r.Since.Equal(want) {
			t.Errorf("ParseRange(%q).Since = %v, want %v", tt.in, r.Since, want)
		}
	}
}
