package store_test

import (
	"context"
	"testing"
	"time"
)

// Priority lists: the song playing now, then the upcoming songs in queue
// order, come before every other candidate (lyrics lookups and
// translation claims).
func TestPGPriorityOrder(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	for _, id := range []string{"recent", "now", "next1", "next2"} {
		seed(t, pool, id, time.Minute)
	}
	exec(t, pool, `DELETE FROM listening_history WHERE track_id IN ('next1', 'next2')`) // queued, never played
	for _, id := range []string{"recent", "now", "next1", "next2"} {
		if _, err := st.EnqueueTranslation(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.ClaimTranslations(ctx, "w1", []string{"now", "next2", "next1"}, 10*time.Minute, 4)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range got {
		order = append(order, c.TrackID)
	}
	if want := []string{"now", "next2", "next1", "recent"}; len(order) != 4 || order[0] != want[0] ||
		order[1] != want[1] || order[2] != want[2] || order[3] != want[3] {
		t.Fatalf("claim order = %v, want %v", order, want)
	}

	// Lyrics lookups: drop the lyrics so all four need one.
	exec(t, pool, `TRUNCATE lyrics_translations, lyrics CASCADE`)
	cands, err := st.TracksNeedingLyrics(ctx, 4, []string{"next1", "now"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 4 || cands[0].TrackID != "next1" || cands[1].TrackID != "now" {
		t.Fatalf("lyrics order = %+v", cands)
	}
	if _, err := st.TracksNeedingLyrics(ctx, 4, nil); err != nil {
		t.Fatalf("nil priority: %v", err)
	}
}
