package config

import (
	"testing"
	"time"
)

func setPollerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("INTERNAL_API_TOKEN", "0123456789abcdef")
}

func TestQueuePrefetchDefaults(t *testing.T) {
	setPollerEnv(t)
	cfg, err := Load[Poller]()
	if err != nil {
		t.Fatal(err)
	}
	q := cfg.QueuePrefetch
	if !q.Enabled || q.Depth != 1 || q.Recheck != time.Minute || q.TTL != 6*time.Hour {
		t.Fatalf("poller defaults = %+v", q)
	}
	setAPIEnv(t)
	api, err := Load[API]()
	if err != nil {
		t.Fatal(err)
	}
	if p := api.Prefetch; !p.Translate || p.Enrich || !p.Details {
		t.Fatalf("api defaults = %+v", p)
	}
}

func TestQueuePrefetchValidation(t *testing.T) {
	cases := []struct {
		key, value string
		ok         bool
	}{
		{"QUEUE_PREFETCH_DEPTH", "5", true},
		{"QUEUE_PREFETCH_DEPTH", "0", false},
		{"QUEUE_PREFETCH_DEPTH", "6", false},
		{"QUEUE_PREFETCH_RECHECK", "15s", true},
		{"QUEUE_PREFETCH_RECHECK", "10s", false},
		{"QUEUE_PREFETCH_RECHECK", "11m", false},
		{"QUEUE_PREFETCH_TTL", "1m", true},
		{"QUEUE_PREFETCH_TTL", "30s", false},
		{"QUEUE_PREFETCH_TTL", "49h", false},
	}
	for _, c := range cases {
		t.Run(c.key+"="+c.value, func(t *testing.T) {
			setPollerEnv(t)
			t.Setenv(c.key, c.value)
			_, err := Load[Poller]()
			if (err == nil) != c.ok {
				t.Fatalf("err = %v, want ok=%v", err, c.ok)
			}
		})
	}
	// Disabled: the other values are not checked.
	setPollerEnv(t)
	t.Setenv("QUEUE_PREFETCH_ENABLED", "false")
	t.Setenv("QUEUE_PREFETCH_DEPTH", "0")
	if _, err := Load[Poller](); err != nil {
		t.Fatalf("disabled prefetch rejected: %v", err)
	}
}
