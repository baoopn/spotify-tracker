package backupstatus

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		time       string
		days, min  int
		wantFields []string
	}{
		{"03:00", 30, 7, nil},
		{"00:00", 0, 1, nil},
		{"23:59", 3650, 1000, nil},
		{"24:00", 30, 7, []string{"time"}},
		{"3:00", 30, 7, []string{"time"}},
		{"03:60", 30, 7, []string{"time"}},
		{"", 30, 7, []string{"time"}},
		{"03:00 ", 30, 7, []string{"time"}},
		{"03:00", -1, 7, []string{"keep_days"}},
		{"03:00", 3651, 7, []string{"keep_days"}},
		{"03:00", 30, 0, []string{"keep_min"}},
		{"03:00", 30, 1001, []string{"keep_min"}},
		{"x", -1, 0, []string{"time", "keep_days", "keep_min"}},
	}
	for _, tt := range tests {
		got := Validate(tt.time, tt.days, tt.min)
		if len(got) != len(tt.wantFields) {
			t.Errorf("Validate(%q,%d,%d) = %v, want fields %v", tt.time, tt.days, tt.min, got, tt.wantFields)
			continue
		}
		for _, f := range tt.wantFields {
			if got[f] == "" {
				t.Errorf("Validate(%q,%d,%d): no message for %s", tt.time, tt.days, tt.min, f)
			}
		}
	}
}
