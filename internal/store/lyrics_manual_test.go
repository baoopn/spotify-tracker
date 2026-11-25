package store

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/migrations"
)

// Automatic lookups never claim held manual lyrics, and held rows are never
// shown as waiting or queued.
func TestManualHeldSQL(t *testing.T) {
	wantSQL(t, lyricsHeldSQL, "l.source = 'manual' AND NOT l.replace_manual")
	for name, s := range map[string]string{"claimable": lyricsClaimableSQL, "waiting": lyricsWaitingSQL,
		"queued": lyricsQueuedSQL} {
		if !strings.Contains(s, "NOT "+lyricsHeldSQL) {
			t.Errorf("%s does not exclude held manual lyrics: %s", name, s)
		}
	}
}

// SetLyrics locks the row, gives the lease back on held manual lyrics and
// stores only otherwise (resetting the manual columns).
func TestSetLyricsManualSQL(t *testing.T) {
	db := &captureDB{}
	_, _, _ = (&Queries{db: db}).SetLyrics(context.Background(), "w1", "t1", domain.Lyrics{Status: domain.LyricsFound, Plain: "p"})
	wantSQL(t, db.sql, "FOR UPDATE", lyricsHeldSQL+" AS held",
		"kept AS (\n\t\t\tUPDATE lyrics l SET claimed_by = NULL, claimed_until = NULL, claim_requested_at = NULL",
		"AND old.held", "AND NOT old.held", "replace_manual = false, manual_at = NULL, manual_by = NULL",
		"source = 'lrclib'")
}

// DeferLyrics leaves held manual lyrics as they are (only the lease goes),
// and ends a replace_manual request unless the lookup failed.
func TestDeferLyricsManualSQL(t *testing.T) {
	db := &captureDB{}
	_ = (&Queries{db: db}).DeferLyrics(context.Background(), "w1", "t1", domain.LyricsError, time.Now(), "x")
	wantSQL(t, db.sql, "attempts = CASE WHEN "+lyricsHeldSQL+" THEN l.attempts",
		"next_attempt_at = CASE WHEN "+lyricsHeldSQL+" THEN l.next_attempt_at",
		"last_error = CASE WHEN "+lyricsHeldSQL+" THEN l.last_error ELSE $5 END",
		"replace_manual = l.replace_manual AND $3::text = 'error'")
}

func TestRequeueLyricsManualSQL(t *testing.T) {
	db := &captureDB{}
	_, _ = (&Queries{db: db}).RequeueLyrics(context.Background(), "t1", true)
	wantSQL(t, db.sql, "replace_manual = lyrics.source = 'manual'", "WHERE lyrics.source <> 'manual' OR $2::boolean")
	if len(db.args) != 2 || db.args[1] != true {
		t.Errorf("args = %v", db.args)
	}
}

func TestSetManualLyricsSQL(t *testing.T) {
	db := &captureDB{}
	_, _, err := (&Queries{db: db}).SetManualLyrics(context.Background(), "t1",
		domain.Lyrics{Status: domain.LyricsInstrumental, Plain: "dropped"}, "me@x")
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "INSERT INTO lyrics AS l (track_id, status, source, plain_lyrics, synced_lyrics, manual_at, manual_by)",
		"'manual'", "requested_at = NULL, claim_requested_at = NULL, replace_manual = false",
		"l.plain_lyrics IS DISTINCT FROM EXCLUDED.plain_lyrics", "UPDATE lyrics_translations tr SET requested_at = now()",
		"up.hash IS DISTINCT FROM old.hash", "up.hash <> tr.source_hash",
		"FROM tracks t LEFT JOIN old ON old.track_id = t.id") // up reads old first: old has the hash from before
	if strings.Contains(db.sql, "claimed_by") || strings.Contains(db.sql, "INSERT INTO lyrics_translations") {
		t.Error("a manual set must keep a live claim and never create a translation row")
	}
	if len(db.args) != 5 || db.args[1] != domain.LyricsInstrumental || strArg(db.args[2]) != nil || db.args[4] != "me@x" {
		t.Errorf("args = %v", db.args)
	}
	db = &captureDB{}
	if _, _, err := (&Queries{db: db}).SetManualLyrics(context.Background(), "t1", domain.Lyrics{Status: domain.LyricsNotFound}, ""); err == nil || db.sql != "" {
		t.Errorf("not_found = %v", err)
	}
}

func TestClearManualLyricsSQL(t *testing.T) {
	db := &captureDB{}
	_, _ = (&Queries{db: db}).ClearManualLyrics(context.Background(), "t1")
	wantSQL(t, db.sql, "status = 'pending', source = 'lrclib'", "requested_at = now()", "l.source = 'manual'",
		lookupableSQL, "DELETE FROM lyrics l USING tracks t")
}

func TestManualLyricsMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00019_manual_lyrics.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	if !ok {
		t.Fatal("no Down section")
	}
	wantSQL(t, up, "ADD COLUMN manual_at timestamptz", "ADD COLUMN manual_by text",
		"ADD COLUMN replace_manual boolean NOT NULL DEFAULT false", "CHECK (source IN ('lrclib', 'manual'))")
	wantSQL(t, down, "DROP CONSTRAINT IF EXISTS lyrics_manual_check", "UPDATE lyrics SET source = 'lrclib' WHERE source = 'manual'",
		"DROP COLUMN replace_manual")
}
