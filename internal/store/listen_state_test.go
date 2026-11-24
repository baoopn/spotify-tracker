package store

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/migrations"
)

func TestListenStateMigration(t *testing.T) {
	b, err := fs.ReadFile(migrations.FS, "00016_listen_state.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(b), "-- +goose Down")
	if !ok {
		t.Fatal("no Down section")
	}
	wantSQL(t, up, "CREATE TABLE listen_state", "PRIMARY KEY DEFAULT true CHECK (id)",
		"row_played_at", "applied_at", "INSERT INTO listen_state (id) VALUES (true)")
	wantSQL(t, down, "DROP TABLE IF EXISTS listen_state")
}

// The listen is read with a row lock so load, decide and save are atomic.
func TestLockListenStateLocksTheRow(t *testing.T) {
	db := &captureDB{}
	_, err := (&Queries{db: db}).LockListenState(context.Background())
	if !errors.Is(err, errCaptured) {
		t.Fatalf("err = %v", err)
	}
	wantSQL(t, db.sql, "FROM listen_state", "FOR UPDATE")
}

func TestSaveListenStateUpserts(t *testing.T) {
	db := &captureDB{}
	p := int32(5)
	_ = (&Queries{db: db}).SaveListenState(context.Background(), ListenState{TrackID: "a", ProgressMS: &p, Counted: true})
	wantSQL(t, db.sql, "INSERT INTO listen_state", "ON CONFLICT (id) DO UPDATE")
	if db.args[0] != "a" || db.args[1] != (*time.Time)(nil) {
		t.Fatalf("args = %v (zero times must be NULL)", db.args)
	}
}

func TestIncrementReplayAtTargetsOneRow(t *testing.T) {
	db := &captureDB{}
	at := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	_, _ = (&Queries{db: db}).IncrementReplayAt(context.Background(), "a", at, at)
	wantSQL(t, db.sql, "UPDATE listening_history SET replay_count = replay_count + 1",
		"WHERE track_id = $1 AND played_at = $2")
}

// Going idle must not reset the listen (resume would count again).
func TestMarkNotPlayingKeepsTheListen(t *testing.T) {
	db := &captureDB{}
	_, _ = (&Queries{db: db}).MarkNotPlaying(context.Background(), time.Now())
	if strings.Contains(db.sql, "listen_state") {
		t.Fatalf("MarkNotPlaying touches listen_state:\n%s", db.sql)
	}
}
