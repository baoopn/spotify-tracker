package store

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/migrations"
)

func TestTranslationsMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00015_lyrics_translations.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	if !ok {
		t.Fatal("no Down section")
	}
	wantSQL(t, up, "CREATE TABLE lyrics_translations", "REFERENCES lyrics (track_id) ON DELETE CASCADE",
		"'pending', 'done', 'skipped', 'refused', 'error'", "claimed_by", "claimed_until", "requested_at",
		"CREATE TABLE translation_usage")
	wantSQL(t, down, "DROP TABLE IF EXISTS translation_usage", "DROP TABLE IF EXISTS lyrics_translations")
}

// Songs are queued one by one (playing now) or by a recent play; nothing
// selects the whole library.
func TestEnqueueTranslationSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).EnqueueTranslation(context.Background(), "t1"); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "INSERT INTO lyrics_translations", "'pending'", "l.track_id = $1 AND l.status = 'found'",
		"ON CONFLICT (track_id) DO NOTHING",
		// true only when the row exists afterwards (no lyrics yet: false)
		"SELECT EXISTS (SELECT 1 FROM ins) OR EXISTS (SELECT 1 FROM lyrics_translations WHERE track_id = $1)")
	if len(db.args) != 1 || db.args[0] != "t1" {
		t.Errorf("args = %v", db.args)
	}

	db = &captureDB{}
	if _, err := (&Queries{db: db}).EnqueueRecentTranslations(context.Background(), 30*time.Minute); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "l.status = 'found' AND EXISTS (SELECT 1 FROM listening_history h",
		"h.played_at >= now() - $1::interval", "ON CONFLICT (track_id) DO NOTHING")
	if len(db.args) != 1 || db.args[0] != 30*time.Minute {
		t.Errorf("args = %v", db.args)
	}
}

// The claim takes the priority song first, then re-queued songs, then the
// last played; it skips live leases and locked rows and takes expired ones.
func TestClaimTranslationsSQL(t *testing.T) {
	db := &captureDB{}
	_, err := (&Queries{db: db}).ClaimTranslations(context.Background(), "w1", "now", 10*time.Minute, 1)
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql,
		"(tr.claimed_until IS NULL OR tr.claimed_until < now())",
		"tr.status = 'pending' OR tr.requested_at IS NOT NULL OR tr.source_hash <> "+srcHashSQL,
		"tr.status = 'error' AND tr.next_attempt_at <= now()",
		"ORDER BY (tr.track_id = $2) DESC, tr.requested_at DESC NULLS LAST, p.last_played DESC NULLS LAST",
		"FOR UPDATE OF tr SKIP LOCKED",
		"claim_requested_at = tr.requested_at, requested_at = NULL",
		"JOIN lyrics l ON l.track_id = tr.track_id AND l.status = 'found'")
	if strings.Contains(db.sql, "LEFT JOIN lyrics_translations") {
		t.Error("the claim must read queued rows only (no backfill)")
	}
	if len(db.args) != 4 || db.args[0] != "w1" || db.args[1] != "now" || db.args[2] != 10*time.Minute || db.args[3] != 1 {
		t.Errorf("args = %v", db.args)
	}
}

func TestSetTranslationSQL(t *testing.T) {
	db := &captureDB{}
	_, err := (&Queries{db: db}).SetTranslation(context.Background(), "w1", "t1", "h",
		domain.Translation{Kind: domain.TranslationSynced, SourceLanguage: "vi", EN: []string{"a", ""}})
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "status = CASE WHEN tr.requested_at IS NULL THEN 'done' ELSE 'pending' END",
		"WHERE tr.track_id = $2 AND tr.claimed_by = $1",
		"l.status = 'found' AND "+srcHashSQL+" = $3", "claimed_by = NULL, claimed_until = NULL")
	if db.args[0] != "w1" || db.args[1] != "t1" || db.args[2] != "h" {
		t.Errorf("args = %v", db.args)
	}
	if vi, _ := db.args[6].([]string); vi != nil {
		t.Errorf("vi = %v, want NULL", db.args[6])
	}

	db = &captureDB{}
	if _, err := (&Queries{db: db}).SetTranslation(context.Background(), "w1", "t1", "h", domain.Translation{}); err == nil || db.sql != "" {
		t.Errorf("no kind = %v; want a validation error before the DB", err)
	}
}

func TestDeferAndReleaseTranslationSQL(t *testing.T) {
	db := &captureDB{}
	next := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	err := (&Queries{db: db}).DeferTranslation(context.Background(), "w1", "t1", "h", domain.TranslationError, &next, "boom")
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "WHEN tr.status = $4 AND tr.source_hash = $3 THEN tr.attempts + 1 ELSE 1 END",
		"status = CASE WHEN tr.requested_at IS NULL THEN $4 ELSE 'pending' END",
		"WHERE tr.track_id = $2 AND tr.claimed_by = $1", "claimed_by = NULL, claimed_until = NULL")
	if db.args[4] != &next || deref(strArg(db.args[5])) != "boom" {
		t.Errorf("args = %v", db.args)
	}
	db = &captureDB{}
	if err := (&Queries{db: db}).DeferTranslation(context.Background(), "w1", "t1", "h", domain.TranslationDone, nil, ""); err == nil || db.sql != "" {
		t.Errorf("done = %v; want a validation error before the DB", err)
	}

	db = &captureDB{}
	_ = (&Queries{db: db}).ReleaseTranslation(context.Background(), "w1", "t1")
	wantSQL(t, db.sql, "SET claimed_by = NULL, claimed_until = NULL",
		"requested_at = COALESCE(requested_at, claim_requested_at)", "track_id = $2 AND claimed_by = $1")
}

func TestRequeueTranslationSQL(t *testing.T) {
	db := &captureDB{}
	if _, err := (&Queries{db: db}).RequeueTranslation(context.Background(), "t1"); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "l.track_id = $1 AND l.status = 'found'",
		"status = CASE WHEN lyrics_translations.claimed_until >= now() THEN lyrics_translations.status",
		"attempts = 0", "next_attempt_at = NULL", "requested_at = now()")
	if strings.Contains(db.sql, "claimed_by = NULL") {
		t.Error("re-queue must not clear a live lease")
	}
}

func TestTranslationQueueSummarySQL(t *testing.T) {
	db := &captureDB{}
	now := time.Date(2026, 11, 24, 23, 30, 0, 0, time.FixedZone("x", -3*3600))
	if _, err := (&Queries{db: db}).TranslationQueueSummary(context.Background(), now); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	wantSQL(t, db.sql, "tr.claimed_until IS NOT NULL AND tr.claimed_until >= now()",
		"status = 'error' AND next_attempt_at IS NULL", "min(next_attempt_at)",
		"date_trunc('month', $1::date)")
	if day, _ := db.args[0].(time.Time); !day.Equal(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("day = %v (want the UTC day)", db.args[0])
	}
}

// The cap check takes a transaction-level advisory lock first, so replicas
// cannot both take the last call.
func TestReserveTranslationCallSQL(t *testing.T) {
	db := &allSQL{}
	_, _ = (&Queries{db: db}).reserveTranslationCall(context.Background(), time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC), 100, 1000)
	if len(db.sqls) != 2 || db.sqls[0] != translationUsageLock {
		t.Fatalf("statements = %q", db.sqls)
	}
	wantSQL(t, db.sqls[0], "pg_advisory_xact_lock")
	wantSQL(t, db.sqls[1], "($2 = 0 OR d.n < $2) AND ($3 = 0 OR m.n < $3)", "ON CONFLICT (day) DO UPDATE SET calls = translation_usage.calls + 1")
	if db.args[1] != 100 || db.args[2] != 1000 {
		t.Errorf("args = %v", db.args)
	}
}

// allSQL records every statement; Exec succeeds, queries fail.
type allSQL struct {
	captureDB
	sqls []string
	args []any
}

func (a *allSQL) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	a.sqls = append(a.sqls, sql)
	return pgconn.CommandTag{}, nil
}

func (a *allSQL) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	a.sqls, a.args = append(a.sqls, sql), args
	return errRow{}
}

func TestTranslationState(t *testing.T) {
	s := func(v string) *string { return &v }
	for _, c := range []struct {
		status         *string
		fresh, waiting bool
		want           string
	}{
		{nil, false, true, domain.TranslationPending},    // not queued yet, no cap reached
		{nil, false, false, ""},                          // cap reached: not on its way
		{s("done"), true, false, domain.TranslationDone}, // current
		{s("done"), false, true, domain.TranslationPending},
		{s("done"), false, false, ""}, // stale but capped
		{s("pending"), true, true, domain.TranslationPending},
		{s("error"), true, true, domain.TranslationPending}, // retry within minutes
		{s("error"), true, false, ""},                       // retry far away, or given up
		{s("refused"), true, false, ""},
		{s("skipped"), true, false, ""},
	} {
		if got := translationState(c.status, c.fresh, c.waiting); got != c.want {
			t.Errorf("%v fresh=%v waiting=%v = %q, want %q", deref(c.status), c.fresh, c.waiting, got, c.want)
		}
	}
}

// Pending must end: a far retry or a reached cap does not count as waiting,
// a live lease always does.
func TestTranslationWaitingSQL(t *testing.T) {
	if !strings.Contains(trWaitingSQL("false"), "WHEN tr.track_id IS NULL THEN false") {
		t.Error("admin: a track with no row is not waiting (no backfill)")
	}
	wantSQL(t, trWaitingSQL("NOT cap.reached"), "WHEN tr.track_id IS NULL THEN NOT cap.reached", "tr.requested_at IS NOT NULL",
		"tr.claimed_until IS NOT NULL AND tr.claimed_until >= now()",
		"tr.status = 'error' AND tr.next_attempt_at <= now() + interval '2 minutes'",
		"AND NOT cap.reached")
	wantSQL(t, capReachedSQL("$3", "$4"), "($3 > 0 AND", ">= $3", "($4 > 0 AND COALESCE(SUM(u.calls), 0) >= $4)",
		"date_trunc('month', (now() AT TIME ZONE 'UTC')::date)")
}

// Synced translations are joined with newlines, so the admin SPA splits
// them back into one line per LRC line.
func TestJoinedTranslation(t *testing.T) {
	lines := []string{"one", "", "three"}
	got := joinedTranslation(lines, nil)
	if got == nil || len(strings.Split(*got, "\n")) != len(lines) {
		t.Errorf("joined = %v", got)
	}
	p := "plain"
	if got := joinedTranslation(nil, &p); got != &p {
		t.Error("plain must pass through")
	}
	if joinedTranslation(nil, nil) != nil {
		t.Error("none must be nil")
	}
}
