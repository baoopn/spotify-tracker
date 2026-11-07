package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

const topPlaylistColumns = `period, period_start, period_end, time_zone, name, status,
	COALESCE(spotify_playlist_id, ''), COALESCE(spotify_url, ''), track_count, total_plays,
	attempts, COALESCE(last_error, ''), next_attempt_at, published_at, hidden, live, refreshed_at, created_at, updated_at`

func topPlaylistTargets(p *domain.TopPlaylistRecord) []any {
	return []any{&p.Period, &p.PeriodStart, &p.PeriodEnd, &p.TimeZone, &p.Name, &p.Status,
		&p.SpotifyPlaylistID, &p.SpotifyURL, &p.TrackCount, &p.TotalPlays, &p.Attempts,
		&p.LastError, &p.NextAttemptAt, &p.PublishedAt, &p.Hidden, &p.Live, &p.RefreshedAt, &p.CreatedAt, &p.UpdatedAt}
}

// TopPlaylist returns the generation state of one period's top playlist,
// or domain.ErrNotFound when it was never attempted.
func (q *Queries) TopPlaylist(ctx context.Context, period string, start time.Time) (domain.TopPlaylistRecord, error) {
	var p domain.TopPlaylistRecord
	err := q.db.QueryRow(ctx, `SELECT `+topPlaylistColumns+` FROM top_playlists
		WHERE period = $1 AND period_start = $2`, period, start).Scan(topPlaylistTargets(&p)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, domain.ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("top playlist %s %s: %w", period, start.Format(time.RFC3339), err)
	}
	return p, nil
}

// UpsertTopPlaylist writes a top playlist's state. A known Spotify playlist
// ID is never cleared, so retries can't create duplicate playlists.
func (q *Queries) UpsertTopPlaylist(ctx context.Context, p domain.TopPlaylistRecord) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO top_playlists (period, period_start, period_end, time_zone, name, status,
			spotify_playlist_id, spotify_url, track_count, total_plays, attempts, last_error,
			next_attempt_at, published_at, live, refreshed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (period, period_start) DO UPDATE SET
			period_end = EXCLUDED.period_end, time_zone = EXCLUDED.time_zone,
			name = EXCLUDED.name, status = EXCLUDED.status,
			spotify_playlist_id = COALESCE(EXCLUDED.spotify_playlist_id, top_playlists.spotify_playlist_id),
			spotify_url = COALESCE(EXCLUDED.spotify_url, top_playlists.spotify_url),
			track_count = EXCLUDED.track_count, total_plays = EXCLUDED.total_plays,
			attempts = EXCLUDED.attempts, last_error = EXCLUDED.last_error,
			next_attempt_at = EXCLUDED.next_attempt_at, published_at = EXCLUDED.published_at,
			live = EXCLUDED.live, refreshed_at = EXCLUDED.refreshed_at`,
		p.Period, p.PeriodStart, p.PeriodEnd, p.TimeZone, p.Name, p.Status,
		nullIfEmpty(p.SpotifyPlaylistID), nullIfEmpty(p.SpotifyURL), p.TrackCount, p.TotalPlays,
		p.Attempts, nullIfEmpty(p.LastError), p.NextAttemptAt, p.PublishedAt, p.Live, p.RefreshedAt)
	if err != nil {
		return fmt.Errorf("upsert top playlist %s %s: %w", p.Period, p.PeriodStart.Format(time.RFC3339), err)
	}
	return nil
}

// SaveTopPlaylistSnapshot upserts a top playlist and replaces its ranked
// tracks in one transaction.
func (s *Store) SaveTopPlaylistSnapshot(ctx context.Context, p domain.TopPlaylistRecord, tracks []domain.RankedTrack) error {
	return s.InTx(ctx, func(q *Queries) error {
		if err := q.UpsertTopPlaylist(ctx, p); err != nil {
			return err
		}
		if _, err := q.db.Exec(ctx, `DELETE FROM top_playlist_tracks WHERE period = $1 AND period_start = $2`,
			p.Period, p.PeriodStart); err != nil {
			return fmt.Errorf("clear top playlist tracks: %w", err)
		}
		if len(tracks) == 0 {
			return nil
		}
		b := &pgx.Batch{}
		for i, t := range tracks {
			b.Queue(`INSERT INTO top_playlist_tracks (period, period_start, position, track_id, plays)
				VALUES ($1, $2, $3, $4, $5)`, p.Period, p.PeriodStart, i+1, t.TrackID, t.Plays)
		}
		if err := q.db.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("insert top playlist tracks: %w", err)
		}
		return nil
	})
}

// TopPlaylistSnapshot returns a top playlist's stored ranking (track IDs
// and plays in position order), without hydrating the tracks.
func (q *Queries) TopPlaylistSnapshot(ctx context.Context, period string, start time.Time) ([]domain.RankedTrack, error) {
	rows, err := q.db.Query(ctx, `SELECT track_id, plays FROM top_playlist_tracks
		WHERE period = $1 AND period_start = $2
		ORDER BY position`, period, start)
	if err != nil {
		return nil, fmt.Errorf("top playlist snapshot: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.RankedTrack, error) {
		var t domain.RankedTrack
		err := r.Scan(&t.TrackID, &t.Plays)
		return t, err
	})
}

// RankTracks returns the most played tracks with played_at in [start, end),
// by total plays (replays included), then most recently played.
func (q *Queries) RankTracks(ctx context.Context, start, end time.Time, limit int) ([]domain.RankedTrack, error) {
	rows, err := q.db.Query(ctx, `
		SELECT track_id, SUM(replay_count)::bigint AS plays
		FROM listening_history
		WHERE played_at >= $1 AND played_at < $2
		GROUP BY track_id
		ORDER BY plays DESC, MAX(played_at) DESC, track_id
		LIMIT $3`, start, end, limit)
	if err != nil {
		return nil, fmt.Errorf("rank tracks: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.RankedTrack, error) {
		var t domain.RankedTrack
		err := r.Scan(&t.TrackID, &t.Plays)
		return t, err
	})
}

// PublishedTopPlaylists lists created, visible top playlists of one period
// kind, newest first.
func (q *Queries) PublishedTopPlaylists(ctx context.Context, period string, limit int) ([]domain.TopPlaylistRecord, error) {
	rows, err := q.db.Query(ctx, `SELECT `+topPlaylistColumns+` FROM top_playlists
		WHERE period = $1 AND status = 'created' AND NOT hidden
		ORDER BY period_start DESC
		LIMIT $2`, period, limit)
	if err != nil {
		return nil, fmt.Errorf("published top playlists: %w", err)
	}
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

// EndedLiveTopPlaylists lists created live top playlists whose period ended
// at or before endedBy and that are not backing off a failed finalization at
// now, oldest period first.
func (q *Queries) EndedLiveTopPlaylists(ctx context.Context, endedBy, now time.Time, limit int) ([]domain.TopPlaylistRecord, error) {
	rows, err := q.db.Query(ctx, `SELECT `+topPlaylistColumns+` FROM top_playlists
		WHERE status = 'created' AND live AND period_end <= $1
			AND (attempts = 0 OR next_attempt_at IS NULL OR next_attempt_at <= $2)
		ORDER BY period_start, period
		LIMIT $3`, endedBy, now, limit)
	if err != nil {
		return nil, fmt.Errorf("ended live top playlists: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.TopPlaylistRecord, error) {
		var p domain.TopPlaylistRecord
		err := r.Scan(topPlaylistTargets(&p)...)
		return p, err
	})
}

// AdminTopPlaylists lists top playlists of every status (period "" = every
// kind), newest period first, with the total row count.
func (q *Queries) AdminTopPlaylists(ctx context.Context, period string, limit, offset int) ([]domain.TopPlaylistRecord, int, error) {
	var total int
	if err := q.db.QueryRow(ctx, `SELECT COUNT(*) FROM top_playlists
		WHERE $1::text = '' OR period = $1`, period).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("admin top playlists count: %w", err)
	}
	rows, err := q.db.Query(ctx, `SELECT `+topPlaylistColumns+` FROM top_playlists
		WHERE $1::text = '' OR period = $1
		ORDER BY period_start DESC, period
		LIMIT $2 OFFSET $3`, period, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("admin top playlists: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.TopPlaylistRecord, error) {
		var p domain.TopPlaylistRecord
		err := r.Scan(topPlaylistTargets(&p)...)
		return p, err
	})
	if out == nil {
		out = []domain.TopPlaylistRecord{}
	}
	return out, total, err
}

// SetTopPlaylistHidden shows or hides a top playlist on the public page and
// returns the updated row (domain.ErrNotFound when it does not exist).
func (q *Queries) SetTopPlaylistHidden(ctx context.Context, period string, start time.Time, hidden bool) (domain.TopPlaylistRecord, error) {
	var p domain.TopPlaylistRecord
	err := q.db.QueryRow(ctx, `UPDATE top_playlists SET hidden = $3
		WHERE period = $1 AND period_start = $2
		RETURNING `+topPlaylistColumns, period, start, hidden).Scan(topPlaylistTargets(&p)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, domain.ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("set top playlist hidden %s %s: %w", period, start.Format(time.RFC3339), err)
	}
	return p, nil
}

// TopPlaylistTracks returns a top playlist's snapshot with hydrated tracks.
func (q *Queries) TopPlaylistTracks(ctx context.Context, period string, start time.Time) ([]domain.TopPlaylistTrack, error) {
	rows, err := q.db.Query(ctx, `
		SELECT tp.position, tp.plays, `+trackColumns+`
		FROM top_playlist_tracks tp
		JOIN tracks t ON t.id = tp.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE tp.period = $1 AND tp.period_start = $2
		ORDER BY tp.position`, period, start)
	if err != nil {
		return nil, fmt.Errorf("top playlist tracks: %w", err)
	}
	defer rows.Close()
	out := []domain.TopPlaylistTrack{}
	for rows.Next() {
		var tt domain.TopPlaylistTrack
		var pos int16
		var al albumScan
		if err := rows.Scan(append(append([]any{&pos, &tt.Plays},
			scanTrack(&tt.Track)...), al.targets()...)...); err != nil {
			return nil, err
		}
		tt.Position = int(pos)
		tt.Track.Album = al.album()
		out = append(out, tt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	views := make([]*domain.TrackView, len(out))
	for i := range out {
		views[i] = &out[i].Track
	}
	return out, q.hydrate(ctx, views...)
}

// topPlaylistsLockKey is the pg advisory lock key for the top playlists
// generator ("topplist" as an int64).
const topPlaylistsLockKey int64 = 0x746f70706c697374

// LockTopPlaylists takes a session-level advisory lock on a dedicated
// connection so two API instances never generate at once. ok is false when
// another instance holds it. unlock releases the lock and the connection.
func (s *Store) LockTopPlaylists(ctx context.Context) (unlock func(), ok bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("top playlists lock: %w", err)
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, topPlaylistsLockKey).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("top playlists lock: %w", err)
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		// Unlock even if the tick's context was cancelled.
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(uctx, `SELECT pg_advisory_unlock($1)`, topPlaylistsLockKey); err != nil {
			// The lock dies with the session: drop the connection instead of reusing it.
			_ = conn.Conn().Close(uctx)
		}
		conn.Release()
	}, true, nil
}
