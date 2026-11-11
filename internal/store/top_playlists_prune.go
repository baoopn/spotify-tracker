package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

func collectTopPlaylists(rows pgx.Rows) ([]domain.TopPlaylistRecord, error) {
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.TopPlaylistRecord, error) {
		var p domain.TopPlaylistRecord
		err := r.Scan(topPlaylistTargets(&p)...)
		return p, err
	})
	if out == nil {
		out = []domain.TopPlaylistRecord{}
	}
	return out, err
}

// CreatedTopPlaylists lists every created top playlist of one period kind
// (live or not, hidden or not), newest period first.
func (q *Queries) CreatedTopPlaylists(ctx context.Context, period string) ([]domain.TopPlaylistRecord, error) {
	rows, err := q.db.Query(ctx, `SELECT `+topPlaylistColumns+` FROM top_playlists
		WHERE period = $1 AND status = 'created'
		ORDER BY period_start DESC`, period)
	if err != nil {
		return nil, fmt.Errorf("created top playlists: %w", err)
	}
	return collectTopPlaylists(rows)
}

// StuckTopPlaylists lists the failed and waiting_auth top playlists of one
// period kind that hold a Spotify playlist, newest period first.
func (q *Queries) StuckTopPlaylists(ctx context.Context, period string) ([]domain.TopPlaylistRecord, error) {
	rows, err := q.db.Query(ctx, `SELECT `+topPlaylistColumns+` FROM top_playlists
		WHERE period = $1 AND status IN ('failed', 'waiting_auth') AND spotify_playlist_id IS NOT NULL
		ORDER BY period_start DESC`, period)
	if err != nil {
		return nil, fmt.Errorf("stuck top playlists: %w", err)
	}
	return collectTopPlaylists(rows)
}

// PruneTopPlaylist marks a top playlist pruned (unpublished, not live, its
// Spotify removal still pending) and deletes its snapshot, in one
// transaction. domain.ErrNotFound when the row does not exist.
func (s *Store) PruneTopPlaylist(ctx context.Context, period string, start, now time.Time) error {
	return s.InTx(ctx, func(q *Queries) error {
		tag, err := q.db.Exec(ctx, `UPDATE top_playlists
			SET status = 'pruned', pruned_at = $3, spotify_removed = false, live = false,
				next_attempt_at = NULL, attempts = 0
			WHERE period = $1 AND period_start = $2`, period, start, now)
		if err != nil {
			return fmt.Errorf("prune top playlist %s %s: %w", period, start.Format(time.RFC3339), err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if _, err := q.db.Exec(ctx, `DELETE FROM top_playlist_tracks WHERE period = $1 AND period_start = $2`,
			period, start); err != nil {
			return fmt.Errorf("delete pruned top playlist tracks: %w", err)
		}
		return nil
	})
}

// PendingUnfollows lists pruned top playlists whose Spotify playlist is not
// confirmed removed yet, oldest first.
func (q *Queries) PendingUnfollows(ctx context.Context, limit int) ([]domain.TopPlaylistRecord, error) {
	rows, err := q.db.Query(ctx, `SELECT `+topPlaylistColumns+` FROM top_playlists
		WHERE status = 'pruned' AND NOT spotify_removed AND spotify_playlist_id IS NOT NULL
		ORDER BY period_start, period
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending top playlist unfollows: %w", err)
	}
	return collectTopPlaylists(rows)
}

// MarkSpotifyRemoved records that Spotify confirmed a pruned playlist's
// removal and clears its last error.
func (q *Queries) MarkSpotifyRemoved(ctx context.Context, period string, start time.Time) error {
	if _, err := q.db.Exec(ctx, `UPDATE top_playlists SET spotify_removed = true, last_error = NULL
		WHERE period = $1 AND period_start = $2 AND status = 'pruned'`, period, start); err != nil {
		return fmt.Errorf("mark top playlist removed: %w", err)
	}
	return nil
}

// SetTopPlaylistError stores (or clears, with "") a row's last error and
// nothing else.
func (q *Queries) SetTopPlaylistError(ctx context.Context, period string, start time.Time, msg string) error {
	if _, err := q.db.Exec(ctx, `UPDATE top_playlists SET last_error = $3
		WHERE period = $1 AND period_start = $2`, period, start, nullIfEmpty(msg)); err != nil {
		return fmt.Errorf("set top playlist error: %w", err)
	}
	return nil
}

// MarkVerified stores now as the last Spotify check of the given rows.
func (q *Queries) MarkVerified(ctx context.Context, keys []domain.TopPlaylistKey, now time.Time) error {
	if len(keys) == 0 {
		return nil
	}
	periods := make([]string, len(keys))
	starts := make([]time.Time, len(keys))
	for i, k := range keys {
		periods[i], starts[i] = k.Period, k.Start
	}
	if _, err := q.db.Exec(ctx, `UPDATE top_playlists t SET verified_at = $3
		FROM unnest($1::text[], $2::timestamptz[]) AS k(period, period_start)
		WHERE t.period = k.period AND t.period_start = k.period_start`, periods, starts, now); err != nil {
		return fmt.Errorf("mark top playlists verified: %w", err)
	}
	return nil
}

// LastTopPlaylistVerify returns the newest verified_at among created rows
// (nil when none was ever checked).
func (q *Queries) LastTopPlaylistVerify(ctx context.Context) (*time.Time, error) {
	var t *time.Time
	err := q.db.QueryRow(ctx, `SELECT MAX(verified_at) FROM top_playlists WHERE status = 'created'`).Scan(&t)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("last top playlist verify: %w", err)
	}
	return t, nil
}

// AllCreatedTopPlaylists lists every created top playlist of every kind,
// oldest period first.
func (q *Queries) AllCreatedTopPlaylists(ctx context.Context) ([]domain.TopPlaylistRecord, error) {
	rows, err := q.db.Query(ctx, `SELECT `+topPlaylistColumns+` FROM top_playlists
		WHERE status = 'created'
		ORDER BY period_start, period`)
	if err != nil {
		return nil, fmt.Errorf("all created top playlists: %w", err)
	}
	return collectTopPlaylists(rows)
}
