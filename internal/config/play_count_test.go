package config

import (
	"testing"
	"time"
)

func setAPIEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("SECRETS_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")
}

func TestPlayCountDefaults(t *testing.T) {
	setAPIEnv(t)
	cfg, err := Load[API]()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PlayCountMinFraction != 0.3 || cfg.PlayResumeWindow != 24*time.Hour || cfg.ReplayResetThreshold != 10*time.Second {
		t.Fatalf("defaults = %v %v %v", cfg.PlayCountMinFraction, cfg.PlayResumeWindow, cfg.ReplayResetThreshold)
	}
}

func TestPlayCountValidation(t *testing.T) {
	cases := []struct {
		key, value string
		ok         bool
	}{
		{"PLAY_COUNT_MIN_FRACTION", "0.5", true},
		{"PLAY_COUNT_MIN_FRACTION", "0.9", true},
		{"PLAY_COUNT_MIN_FRACTION", "1", false},
		{"PLAY_COUNT_MIN_FRACTION", "0.05", true},
		{"PLAY_COUNT_MIN_FRACTION", "0.01", false},
		{"PLAY_COUNT_MIN_FRACTION", "1.5", false},
		{"PLAY_RESUME_WINDOW", "1h", true},
		{"PLAY_RESUME_WINDOW", "30s", false},
		{"PLAY_RESUME_WINDOW", "721h", false},
		{"REPLAY_RESET_THRESHOLD", "0s", false},
		{"REPLAY_RESET_THRESHOLD", "10m", true},
		{"REPLAY_RESET_THRESHOLD", "11m", false},
	}
	for _, c := range cases {
		t.Run(c.key+"="+c.value, func(t *testing.T) {
			setAPIEnv(t)
			t.Setenv(c.key, c.value)
			_, err := Load[API]()
			if (err == nil) != c.ok {
				t.Fatalf("err = %v, want ok=%v", err, c.ok)
			}
		})
	}
}
