package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPGNowPlayingChangedAt(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms) VALUES ('np1', 'Song', 200000)`)
	changed := time.Date(2026, 11, 27, 10, 0, 0, 123000000, time.FixedZone("x", 7*3600))
	fetched := changed.Add(30 * time.Second)
	progress := int32(30000)
	if err := st.SetCurrentPlayback(ctx, "np1", true, &progress, fetched, &changed); err != nil {
		t.Fatal(err)
	}
	np, err := st.NowPlaying(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if np.ChangedAt == nil || !np.ChangedAt.Equal(changed) || np.ChangedAt.Location() != time.UTC {
		t.Errorf("changed_at = %v", np.ChangedAt)
	}
	b, _ := json.Marshal(np)
	if !strings.Contains(string(b), `"changed_at":"2026-11-27T03:00:00.123Z"`) {
		t.Errorf("json = %s", b)
	}
	// Unknown: null in the JSON; a clear keeps the last change.
	if err := st.SetCurrentPlayback(ctx, "np1", true, &progress, fetched, nil); err != nil {
		t.Fatal(err)
	}
	np, _ = st.NowPlaying(ctx)
	if b, _ := json.Marshal(np); np.ChangedAt != nil || !strings.Contains(string(b), `"changed_at":null`) {
		t.Errorf("null json = %s", b)
	}
	if err := st.SetCurrentPlayback(ctx, "np1", true, &progress, fetched, &changed); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkNotPlaying(ctx, fetched.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if np, _ := st.NowPlaying(ctx); np.IsPlaying || np.ChangedAt == nil {
		t.Errorf("after clear = %+v", np)
	}
}
