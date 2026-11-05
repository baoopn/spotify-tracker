// Package heartbeat lets workers publish their status to Redis so the admin
// dashboard can show service health.
package heartbeat

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	prefix = "spotify-tracker:status:"
	ttl    = 24 * time.Hour
)

// Status is the last reported state of a worker.
type Status struct {
	Name        string
	LastSeenAt  time.Time
	LastOKAt    *time.Time
	LastError   string
	LastErrorAt *time.Time
	Detail      string
}

// Reporter writes heartbeats for one worker.
type Reporter struct {
	rdb  *redis.Client
	name string
}

// NewReporter creates a Reporter.
func NewReporter(rdb *redis.Client, name string) *Reporter { return &Reporter{rdb: rdb, name: name} }

// OK records a successful cycle.
func (r *Reporter) OK(ctx context.Context, detail string) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	pipe := r.rdb.Pipeline()
	pipe.HSet(ctx, prefix+r.name, "last_seen_at", now, "last_ok_at", now, "detail", detail)
	pipe.Expire(ctx, prefix+r.name, ttl)
	_, _ = pipe.Exec(ctx)
}

// Fail records a failed cycle.
func (r *Reporter) Fail(ctx context.Context, err error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	msg := err.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	pipe := r.rdb.Pipeline()
	pipe.HSet(ctx, prefix+r.name, "last_seen_at", now, "last_error", msg, "last_error_at", now)
	pipe.Expire(ctx, prefix+r.name, ttl)
	_, _ = pipe.Exec(ctx)
}

// Read returns the last status of a worker.
func Read(ctx context.Context, rdb *redis.Client, name string) (Status, error) {
	m, err := rdb.HGetAll(ctx, prefix+name).Result()
	if err != nil {
		return Status{}, err
	}
	if len(m) == 0 {
		return Status{Name: name}, errors.New("no heartbeat")
	}
	s := Status{Name: name, LastError: m["last_error"], Detail: m["detail"]}
	s.LastSeenAt, _ = time.Parse(time.RFC3339Nano, m["last_seen_at"])
	if t, err := time.Parse(time.RFC3339Nano, m["last_ok_at"]); err == nil {
		s.LastOKAt = &t
	}
	if t, err := time.Parse(time.RFC3339Nano, m["last_error_at"]); err == nil {
		s.LastErrorAt = &t
	}
	return s, nil
}
