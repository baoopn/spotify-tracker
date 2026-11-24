package store

import (
	"context"
	"errors"
	"testing"
)

// The public track detail needs a play: a track only prefetched from the
// queue (or skipped before it counted) is not found, so the queue cannot be
// probed through /api/v1/tracks/{id}.
func TestTrackDetailRequiresPlay(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).TrackDetail(context.Background(), "t1"); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "WHERE t.id = $1 AND EXISTS (SELECT 1 FROM listening_history h WHERE h.track_id = t.id)")
}
