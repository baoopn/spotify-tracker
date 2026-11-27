package listen

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memKV struct {
	mu   sync.Mutex
	m    map[string][]byte
	ttls map[string]time.Duration
}

func (k *memKV) get(_ context.Context, key string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if v, ok := k.m[key]; ok {
		return v, nil
	}
	return nil, errors.New("nil")
}

func (k *memKV) set(_ context.Context, key string, raw []byte, ttl time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key], k.ttls[key] = raw, ttl
	return nil
}

func (k *memKV) list(_ context.Context, prefix string) ([][]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out [][]byte
	for key, v := range k.m {
		if strings.HasPrefix(key, prefix) {
			out = append(out, v)
		}
	}
	return out, nil
}

func (k *memKV) del(_ context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, key)
	return nil
}

func TestPresenceStore(t *testing.T) {
	kv := &memKV{m: map[string][]byte{}, ttls: map[string]time.Duration{}}
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	s := &PresenceStore{kv: kv, now: func() time.Time { return now }}
	ctx := context.Background()
	tr := trackA
	if err := s.Touch(ctx, Presence{ListenerID: 4, Email: "ann@example.com", State: StateFollowing, Country: "VN"}); err != nil {
		t.Fatal(err)
	}
	const key = "spotify-tracker:listen:presence:4"
	if kv.ttls[key] != 90*time.Second {
		t.Errorf("ttl = %v", kv.ttls[key])
	}
	now = now.Add(30 * time.Second)
	if err := s.Touch(ctx, Presence{ListenerID: 4, Email: "ann@example.com", State: StateListening, TrackID: &tr}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("list = %+v %v", got, err)
	}
	p := got[0]
	if p.State != StateListening || p.TrackID == nil || *p.TrackID != tr ||
		!p.Since.Equal(now.Add(-30*time.Second)) || !p.UpdatedAt.Equal(now) {
		t.Errorf("presence = %+v", p)
	}
	if strings.Contains(string(kv.m[key]), "ip") {
		t.Errorf("stored value mentions an IP: %s", kv.m[key])
	}
	if err := s.Delete(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.List(ctx); len(got) != 0 {
		t.Errorf("after delete = %+v", got)
	}
}
