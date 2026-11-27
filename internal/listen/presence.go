package listen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
)

// Presence states.
const (
	StateListening = "listening" // joined, audio playing in sync
	StateFollowing = "following" // page open and allowed, not hearing audio
	StateIdle      = "idle"      // tab hidden or session ended
)

// PresenceTTL is how long a presence lasts without a new ping.
const PresenceTTL = 90 * time.Second

// Presence is one listener on /listen right now. It holds no IP address:
// the country comes from Cloudflare and the device and browser from the
// User-Agent, as in page stats.
type Presence struct {
	ListenerID int64     `json:"listener_id"`
	Email      string    `json:"email"`
	State      string    `json:"state"`
	TrackID    *string   `json:"track_id"`
	Country    string    `json:"country"` // "" when unknown
	Device     string    `json:"device"`
	Browser    string    `json:"browser"`
	Since      time.Time `json:"since"` // first seen while the key lived
	UpdatedAt  time.Time `json:"updated_at"`
}

// presenceKV is the Redis part the presence store uses (tests replace it).
type presenceKV interface {
	get(ctx context.Context, key string) ([]byte, error)
	set(ctx context.Context, key string, raw []byte, ttl time.Duration) error
	list(ctx context.Context, prefix string) ([][]byte, error)
	del(ctx context.Context, key string) error
}

type redisKV struct{ rdb *redis.Client }

func (r redisKV) get(ctx context.Context, key string) ([]byte, error) {
	return r.rdb.Get(ctx, key).Bytes()
}

func (r redisKV) set(ctx context.Context, key string, raw []byte, ttl time.Duration) error {
	return r.rdb.Set(ctx, key, raw, ttl).Err()
}

func (r redisKV) list(ctx context.Context, prefix string) ([][]byte, error) {
	var keys []string
	iter := r.rdb.Scan(ctx, 0, prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil || len(keys) == 0 {
		return nil, err
	}
	vals, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok {
			out = append(out, []byte(s))
		}
	}
	return out, nil
}

func (r redisKV) del(ctx context.Context, key string) error { return r.rdb.Del(ctx, key).Err() }

// PresenceStore keeps who is on /listen right now in Redis.
type PresenceStore struct {
	kv  presenceKV
	now func() time.Time
}

// NewPresenceStore builds a presence store on rdb.
func NewPresenceStore(rdb *redis.Client) *PresenceStore {
	return &PresenceStore{kv: redisKV{rdb}, now: time.Now}
}

func presenceKey(id int64) string {
	return redisx.KeyListenPresencePrefix + strconv.FormatInt(id, 10)
}

// Touch stores p for PresenceTTL. Since keeps the first-seen time while
// the key lives; UpdatedAt is now.
func (s *PresenceStore) Touch(ctx context.Context, p Presence) error {
	key := presenceKey(p.ListenerID)
	now := s.now().UTC().Truncate(time.Second)
	p.Since, p.UpdatedAt = now, now
	if raw, err := s.kv.get(ctx, key); err == nil {
		var old Presence
		if json.Unmarshal(raw, &old) == nil && !old.Since.IsZero() {
			p.Since = old.Since
		}
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := s.kv.set(ctx, key, raw, PresenceTTL); err != nil {
		return fmt.Errorf("store presence: %w", err)
	}
	return nil
}

// List returns every live presence (unordered).
func (s *PresenceStore) List(ctx context.Context) ([]Presence, error) {
	vals, err := s.kv.list(ctx, redisx.KeyListenPresencePrefix)
	if err != nil {
		return nil, fmt.Errorf("list presence: %w", err)
	}
	out := make([]Presence, 0, len(vals))
	for _, raw := range vals {
		var p Presence
		if json.Unmarshal(raw, &p) == nil && p.ListenerID > 0 {
			out = append(out, p)
		}
	}
	return out, nil
}

// Delete removes a listener's presence (on revoke).
func (s *PresenceStore) Delete(ctx context.Context, listenerID int64) error {
	if err := s.kv.del(ctx, presenceKey(listenerID)); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("delete presence: %w", err)
	}
	return nil
}
