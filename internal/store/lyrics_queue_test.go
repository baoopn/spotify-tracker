package store

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/migrations"
)

// allDB records every statement: Exec succeeds, queries fail with
// errCaptured.
type allDB struct {
	sqls []string
	args [][]any
}

func (a *allDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	a.sqls, a.args = append(a.sqls, sql), append(a.args, args)
	return pgconn.NewCommandTag("INSERT 0 0"), nil
}

func (a *allDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	a.sqls, a.args = append(a.sqls, sql), append(a.args, args)
	return nil, errCaptured
}

func (a *allDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	a.sqls, a.args = append(a.sqls, sql), append(a.args, args)
	return errRow{}
}

func (a *allDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

// ClaimLyrics first queues tracks with no row (pending), then leases rows
// with SKIP LOCKED in queue order.
func TestClaimLyricsSQL(t *testing.T) {
	db := &allDB{}
	_, err := (&Queries{db: db}).ClaimLyrics(context.Background(), "w1", []string{"now", "next"}, 10*time.Minute, 5)
	if !errors.Is(err, errCaptured) || len(db.sqls) != 2 {
		t.Fatalf("err = %v, statements = %d", err, len(db.sqls))
	}
	ins, claim := db.sqls[0], db.sqls[1]
	wantSQL(t, ins, "INSERT INTO lyrics (track_id, status, source)", "'pending'",
		"NOT EXISTS (SELECT 1 FROM lyrics l WHERE l.track_id = t.id)", "t.name <> ''",
		"ORDER BY COALESCE(array_position($1::text[], t.id), 2147483647), p.last_played DESC NULLS LAST",
		"LIMIT $2) c\n\t\tORDER BY c.id", "ON CONFLICT (track_id) DO NOTHING")
	if !reflect.DeepEqual(db.args[0], []any{[]string{"now", "next"}, 5}) {
		t.Errorf("insert args = %v", db.args[0])
	}
	wantSQL(t, claim, "FOR UPDATE OF l SKIP LOCKED", lyricsClaimableSQL,
		"ORDER BY COALESCE(array_position($2::text[], l.track_id), 2147483647),",
		"COALESCE(l.requested_at, l.claim_requested_at) DESC NULLS LAST,",
		"p.last_played DESC NULLS LAST, (l.status = 'pending') DESC, l.track_id",
		"claimed_by = $1, claimed_until = now() + $3::interval",
		"claim_requested_at = COALESCE(l.requested_at, l.claim_requested_at), requested_at = NULL", "LIMIT $4",
		"CASE WHEN l.status = 'pending' THEN '' ELSE l.status END")
	if a := db.args[1]; len(a) != 4 || a[0] != "w1" || a[2] != 10*time.Minute || a[3] != 5 {
		t.Errorf("claim args = %v", a)
	}
	wantSQL(t, lyricsClaimableSQL, "l.claimed_until IS NULL OR l.claimed_until < now()",
		"l.status = 'pending' OR (l.requested_at IS NOT NULL OR l.claim_requested_at IS NOT NULL)",
		"OR l.next_attempt_at <= now()")

	// A nil priority is an empty array, not NULL.
	db = &allDB{}
	_, _ = (&Queries{db: db}).ClaimLyrics(context.Background(), "w1", nil, time.Minute, 1)
	if p, ok := db.args[0][0].([]string); !ok || p == nil {
		t.Errorf("nil priority = %#v", db.args[0][0])
	}
}

// SetLyrics writes only under the claim, releases it, and re-queues an
// existing translation when the lyrics changed.
func TestSetLyricsSQL(t *testing.T) {
	db := &captureDB{}
	_, _, err := (&Queries{db: db}).SetLyrics(context.Background(), "w1", "t1", domain.Lyrics{Status: domain.LyricsInstrumental})
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "WHERE l.track_id = $1 AND l.claimed_by = $2",
		"attempts = 0, last_error = NULL, fetched_at = now(), next_attempt_at = NULL",
		"claimed_by = NULL, claimed_until = NULL, claim_requested_at = NULL",
		"UPDATE lyrics_translations tr SET requested_at = now()",
		"up.status = 'found' AND up.hash <> old.hash", "up.hash <> tr.source_hash")
	if strings.Contains(db.sql, "INSERT INTO lyrics_translations") {
		t.Error("SetLyrics must never create a translation row")
	}
	if strings.Contains(strings.ReplaceAll(db.sql, "claim_requested_at", ""), "requested_at = NULL") {
		t.Error("a re-queue during the claim must survive SetLyrics")
	}
	if len(db.args) != 6 || db.args[0] != "t1" || db.args[1] != "w1" || db.args[2] != domain.LyricsInstrumental {
		t.Fatalf("args = %v", db.args)
	}
	if id, ok := db.args[3].(*int64); !ok || id != nil {
		t.Errorf("lrclib_id = %v, want nil", db.args[3])
	}
	if strArg(db.args[4]) != nil || strArg(db.args[5]) != nil {
		t.Errorf("empty texts = %v, %v; want NULL", db.args[4], db.args[5])
	}

	db = &captureDB{}
	_, _, _ = (&Queries{db: db}).SetLyrics(context.Background(), "w1", "t1",
		domain.Lyrics{Status: domain.LyricsFound, LRCLibID: 42, Plain: "p", Synced: "s"})
	if id, _ := db.args[3].(*int64); id == nil || *id != 42 || deref(strArg(db.args[4])) != "p" || deref(strArg(db.args[5])) != "s" {
		t.Errorf("args = %v", db.args)
	}

	db = &captureDB{}
	_, _, err = (&Queries{db: db}).SetLyrics(context.Background(), "w1", "t1", domain.Lyrics{Status: domain.LyricsNotFound})
	if err == nil || errors.Is(err, errCaptured) || db.sql != "" {
		t.Errorf("not_found = %v (sql %q); want a validation error before the DB", err, db.sql)
	}
}

func TestDeferLyricsSQL(t *testing.T) {
	db := &captureDB{}
	next := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	err := (&Queries{db: db}).DeferLyrics(context.Background(), "w1", "t1", domain.LyricsError, next, "boom")
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "WHEN l.status = $3 THEN l.attempts + 1 ELSE 1 END",
		"status = CASE WHEN l.status IN ('found', 'instrumental') THEN l.status ELSE $3 END",
		"WHERE l.track_id = $1 AND l.claimed_by = $2", "claimed_by = NULL, claimed_until = NULL")
	if len(db.args) != 5 || db.args[1] != "w1" || db.args[3] != next || deref(strArg(db.args[4])) != "boom" {
		t.Errorf("args = %v", db.args)
	}
	db = &captureDB{}
	_ = (&Queries{db: db}).DeferLyrics(context.Background(), "w1", "t1", domain.LyricsNotFound, next, "")
	if strArg(db.args[4]) != nil {
		t.Errorf("empty error = %v, want NULL", db.args[4])
	}
	db = &captureDB{}
	if err := (&Queries{db: db}).DeferLyrics(context.Background(), "w1", "t1", domain.LyricsFound, next, ""); err == nil || db.sql != "" {
		t.Errorf("found = %v; want a validation error before the DB", err)
	}
}

// Release keeps a re-queue mark: the track stays first in line.
func TestReleaseLyricsSQL(t *testing.T) {
	db := &captureDB{}
	if err := (&Queries{db: db}).ReleaseLyrics(context.Background(), "w1", "t1"); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "requested_at = COALESCE(requested_at, claim_requested_at)", "track_id = $1 AND claimed_by = $2")
	if len(db.args) != 2 || db.args[0] != "t1" || db.args[1] != "w1" {
		t.Errorf("args = %v", db.args)
	}
}

func TestRequeueLyricsSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).RequeueLyrics(context.Background(), "t1"); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "INSERT INTO lyrics (track_id, status, source, requested_at)",
		"ON CONFLICT (track_id) DO UPDATE SET requested_at = now()", lookupableSQL)
	if strings.Contains(db.sql, "status =") {
		t.Error("a re-queue must keep the stored status (found lyrics stay served)")
	}
}

func TestLyricsQueueSummarySQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).LyricsQueueSummary(context.Background()); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, lyricsQueuedSQL, lyricsLeasedSQL, "max(fetched_at)", "min(next_attempt_at)",
		"NOT EXISTS (SELECT 1 FROM lyrics l WHERE l.track_id = t.id)")
}

func TestLyricsQueueMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00017_lyrics_queue.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	if !ok {
		t.Fatal("no Down section")
	}
	wantSQL(t, up, "'pending', 'found', 'instrumental', 'not_found', 'error'", "ADD COLUMN claimed_by text",
		"ADD COLUMN claimed_until timestamptz", "ADD COLUMN requested_at timestamptz",
		"ADD COLUMN claim_requested_at timestamptz", "fetched_at DROP NOT NULL")
	wantSQL(t, down, "DELETE FROM lyrics WHERE status = 'pending'", "DROP COLUMN claimed_by",
		"fetched_at SET NOT NULL")
}
