package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The context is a read of one track; it never touches the claim or the
// queue state.
func TestTranslationContextSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).TranslationContext(context.Background(), "t1"); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql,
		"array_agg(ar.name ORDER BY ta.position, ar.name)",
		"CROSS JOIN LATERAL unnest(ar.genres) AS g",
		"COALESCE(al.name, ''), COALESCE(al.album_type, ''), COALESCE(al.release_date, '')",
		"tf.genres, t.explicit, t.duration_ms\n",
		"LEFT JOIN albums al ON al.id = t.album_id",
		"LEFT JOIN track_features tf ON tf.track_id = t.id AND tf.status = 'enriched'",
		"WHERE t.id = $1")
	for _, bad := range []string{"UPDATE", "INSERT", "claimed", "FOR UPDATE", "lyrics_translations", "source_language"} {
		if strings.Contains(db.sql, bad) {
			t.Errorf("context query has %q", bad)
		}
	}
	if len(db.args) != 1 || db.args[0] != "t1" {
		t.Errorf("args = %v", db.args)
	}
}
