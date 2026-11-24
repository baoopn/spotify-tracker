package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ListenState is the stored listen in progress (see tracker.Listen). A zero
// time means unknown; an empty TrackID means no listen yet.
type ListenState struct {
	TrackID     string
	StartedAt   time.Time
	ProgressMS  *int32
	ListenedMS  int64
	Counted     bool
	Replay      bool
	RowPlayedAt time.Time
	AppliedAt   time.Time
	LastSeen    time.Time
}

// LockListenState reads the listen in progress and locks its row until the
// transaction ends, so concurrent or retried snapshots are decided one at a
// time. Call it inside InTx.
func (q *Queries) LockListenState(ctx context.Context) (ListenState, error) {
	var (
		st                              ListenState
		started, row, applied, lastSeen *time.Time
	)
	err := q.db.QueryRow(ctx, `
		SELECT track_id, started_at, progress_ms, listened_ms, counted, replay,
		       row_played_at, applied_at, last_seen
		FROM listen_state WHERE id FOR UPDATE`).
		Scan(&st.TrackID, &started, &st.ProgressMS, &st.ListenedMS, &st.Counted, &st.Replay, &row, &applied, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return ListenState{}, nil
	}
	if err != nil {
		return ListenState{}, fmt.Errorf("lock listen state: %w", err)
	}
	st.StartedAt, st.RowPlayedAt, st.AppliedAt, st.LastSeen = timeOrZero(started), timeOrZero(row), timeOrZero(applied), timeOrZero(lastSeen)
	return st, nil
}

// SaveListenState stores the listen in progress.
func (q *Queries) SaveListenState(ctx context.Context, st ListenState) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO listen_state (id, track_id, started_at, progress_ms, listened_ms, counted, replay,
		                          row_played_at, applied_at, last_seen, updated_at)
		VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (id) DO UPDATE SET
			track_id = EXCLUDED.track_id, started_at = EXCLUDED.started_at,
			progress_ms = EXCLUDED.progress_ms, listened_ms = EXCLUDED.listened_ms,
			counted = EXCLUDED.counted, replay = EXCLUDED.replay,
			row_played_at = EXCLUDED.row_played_at, applied_at = EXCLUDED.applied_at,
			last_seen = EXCLUDED.last_seen, updated_at = now()`,
		st.TrackID, nullIfZeroTime(st.StartedAt), st.ProgressMS, st.ListenedMS, st.Counted, st.Replay,
		nullIfZeroTime(st.RowPlayedAt), nullIfZeroTime(st.AppliedAt), nullIfZeroTime(st.LastSeen))
	if err != nil {
		return fmt.Errorf("save listen state: %w", err)
	}
	return nil
}

// IncrementReplayAt bumps the replay counter of the history row of trackID
// played at playedAt. It reports false when there is no such row.
func (q *Queries) IncrementReplayAt(ctx context.Context, trackID string, playedAt, at time.Time) (bool, error) {
	tag, err := q.db.Exec(ctx, `
		UPDATE listening_history SET replay_count = replay_count + 1, updated_at = $3
		WHERE track_id = $1 AND played_at = $2`, trackID, playedAt, at)
	if err != nil {
		return false, fmt.Errorf("increment replay: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func nullIfZeroTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
