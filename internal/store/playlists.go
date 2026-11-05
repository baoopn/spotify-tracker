package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

const playlistColumns = `p.id, p.status = 'ok', COALESCE(p.name, ''), COALESCE(p.description, ''),
	COALESCE(p.owner_id, ''), COALESCE(p.owner_name, ''), COALESCE(p.cover_image_url, ''),
	COALESCE(p.external_url, ''), COALESCE(p.uri, ''), p.public, COALESCE(p.collaborative, false),
	p.tracks_total, COALESCE(p.snapshot_id, ''), p.fetched_at`

func playlistTargets(p *domain.Playlist) []any {
	return []any{&p.ID, &p.Available, &p.Name, &p.Description, &p.OwnerID, &p.OwnerName,
		&p.CoverImageURL, &p.ExternalURL, &p.URI, &p.Public, &p.Collaborative, &p.TracksTotal,
		&p.SnapshotID, &p.FetchedAt}
}

// PlaylistFetchedAt returns when a playlist's metadata was last fetched.
func (q *Queries) PlaylistFetchedAt(ctx context.Context, id string) (time.Time, error) {
	var t time.Time
	err := q.db.QueryRow(ctx, `SELECT fetched_at FROM playlists WHERE id = $1`, id).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, domain.ErrNotFound
	}
	return t, err
}

// UpsertPlaylist stores playlist metadata. An unavailable playlist only
// records the attempt and keeps any previously known details.
func (q *Queries) UpsertPlaylist(ctx context.Context, p domain.Playlist) error {
	if !p.Available {
		_, err := q.db.Exec(ctx, `
			INSERT INTO playlists (id, status, uri, external_url, fetched_at)
			VALUES ($1, 'unavailable', $2, $3, now())
			ON CONFLICT (id) DO UPDATE SET fetched_at = now(),
				status = CASE WHEN playlists.name IS NULL THEN 'unavailable' ELSE playlists.status END`,
			p.ID, nullIfEmpty(p.URI), nullIfEmpty(p.ExternalURL))
		if err != nil {
			return fmt.Errorf("upsert unavailable playlist %s: %w", p.ID, err)
		}
		return nil
	}
	_, err := q.db.Exec(ctx, `
		INSERT INTO playlists (id, status, name, description, owner_id, owner_name, cover_image_url,
			external_url, uri, public, collaborative, tracks_total, snapshot_id, fetched_at)
		VALUES ($1, 'ok', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
		ON CONFLICT (id) DO UPDATE SET status = 'ok',
			name = EXCLUDED.name, description = EXCLUDED.description,
			owner_id = EXCLUDED.owner_id, owner_name = EXCLUDED.owner_name,
			cover_image_url = EXCLUDED.cover_image_url, external_url = EXCLUDED.external_url,
			uri = EXCLUDED.uri, public = EXCLUDED.public, collaborative = EXCLUDED.collaborative,
			tracks_total = EXCLUDED.tracks_total, snapshot_id = EXCLUDED.snapshot_id, fetched_at = now()`,
		p.ID, p.Name, nullIfEmpty(p.Description), nullIfEmpty(p.OwnerID), nullIfEmpty(p.OwnerName),
		nullIfEmpty(p.CoverImageURL), nullIfEmpty(p.ExternalURL), nullIfEmpty(p.URI), p.Public,
		p.Collaborative, p.TracksTotal, nullIfEmpty(p.SnapshotID))
	if err != nil {
		return fmt.Errorf("upsert playlist %s: %w", p.ID, err)
	}
	return nil
}

// RecentPlaylists lists playlists by the last time a track was played from
// them. Unless includePrivate is set, only playlists known to be public are
// returned (so private playlist names never leak to the public page).
func (q *Queries) RecentPlaylists(ctx context.Context, limit int, includePrivate bool) ([]domain.RecentPlaylist, error) {
	filter := "p.status = 'ok' AND p.public IS TRUE"
	if includePrivate {
		filter = "true"
	}
	rows, err := q.db.Query(ctx, `
		WITH ctx AS (
			SELECT context_id, SUM(replay_count)::bigint AS plays, MAX(played_at) AS last_played
			FROM listening_history
			WHERE context_type = 'playlist' AND context_id IS NOT NULL
			GROUP BY context_id
		)
		SELECT c.plays, c.last_played, `+playlistColumns+`
		FROM ctx c JOIN playlists p ON p.id = c.context_id
		WHERE `+filter+`
		ORDER BY c.last_played DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("recent playlists: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.RecentPlaylist, error) {
		var rp domain.RecentPlaylist
		err := r.Scan(append([]any{&rp.Plays, &rp.LastPlayed}, playlistTargets(&rp.Playlist)...)...)
		return rp, err
	})
	if out == nil {
		out = []domain.RecentPlaylist{}
	}
	return out, err
}

// AdminPlaylists lists every playlist ever played from (including
// unavailable ones), newest first, with offset pagination.
func (q *Queries) AdminPlaylists(ctx context.Context, limit, offset int) ([]domain.RecentPlaylist, int64, error) {
	var total int64
	if err := q.db.QueryRow(ctx, `
		SELECT COUNT(DISTINCT context_id) FROM listening_history
		WHERE context_type = 'playlist' AND context_id IS NOT NULL`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("admin playlists count: %w", err)
	}
	rows, err := q.db.Query(ctx, `
		WITH ctx AS (
			SELECT context_id, SUM(replay_count)::bigint AS plays, MAX(played_at) AS last_played
			FROM listening_history
			WHERE context_type = 'playlist' AND context_id IS NOT NULL
			GROUP BY context_id
		)
		SELECT c.plays, c.last_played, c.context_id, COALESCE(p.status = 'ok', false),
			COALESCE(p.name, ''), COALESCE(p.description, ''), COALESCE(p.owner_id, ''),
			COALESCE(p.owner_name, ''), COALESCE(p.cover_image_url, ''),
			COALESCE(p.external_url, 'https://open.spotify.com/playlist/' || c.context_id),
			COALESCE(p.uri, 'spotify:playlist:' || c.context_id), p.public,
			COALESCE(p.collaborative, false), p.tracks_total, COALESCE(p.snapshot_id, ''),
			COALESCE(p.fetched_at, c.last_played)
		FROM ctx c LEFT JOIN playlists p ON p.id = c.context_id
		ORDER BY c.last_played DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("admin playlists: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.RecentPlaylist, error) {
		var rp domain.RecentPlaylist
		err := r.Scan(append([]any{&rp.Plays, &rp.LastPlayed}, playlistTargets(&rp.Playlist)...)...)
		return rp, err
	})
	if out == nil {
		out = []domain.RecentPlaylist{}
	}
	return out, total, err
}
