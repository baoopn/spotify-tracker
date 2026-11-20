package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// AlbumsNeedingDetails returns up to limit album IDs whose details have
// never been looked up, or were last looked up before staleBefore. Albums
// whose lookup was refused (no copyrights stored) are retried weekly. Albums
// never looked up come first, most recently played first.
func (q *Queries) AlbumsNeedingDetails(ctx context.Context, staleBefore time.Time, limit int) ([]string, error) {
	rows, err := q.db.Query(ctx, `
		SELECT al.id FROM albums al
		LEFT JOIN LATERAL (
			SELECT MAX(h.played_at) AS last_played
			FROM tracks t JOIN listening_history h ON h.track_id = t.id
			WHERE t.album_id = al.id
		) p ON true
		WHERE al.details_fetched_at IS NULL OR al.details_fetched_at < $1
			OR (al.copyrights IS NULL AND al.details_fetched_at < now() - interval '7 days')
		ORDER BY al.details_fetched_at NULLS FIRST, p.last_played DESC NULLS LAST, al.id
		LIMIT $2`, staleBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("albums needing details: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// SetAlbumDetails records an album details lookup. A nil d (refused lookup)
// only stamps the lookup time. Otherwise the album fields and the track
// numbers of its tracks are written in one statement; empty values (and nil
// track or disc numbers) keep what is stored. The data-modifying CTE always runs, even when no track matches.
func (q *Queries) SetAlbumDetails(ctx context.Context, id string, d *domain.AlbumDetails) error {
	if d == nil {
		if _, err := q.db.Exec(ctx, `UPDATE albums SET details_fetched_at = now() WHERE id = $1`, id); err != nil {
			return fmt.Errorf("set album details %s: %w", id, err)
		}
		return nil
	}
	copyrights := d.Copyrights
	if copyrights == nil {
		copyrights = []domain.Copyright{}
	}
	cj, err := json.Marshal(copyrights)
	if err != nil {
		return fmt.Errorf("marshal copyrights: %w", err)
	}
	ids := make([]string, 0, len(d.Tracks))
	tns := make([]*int32, 0, len(d.Tracks))
	dns := make([]*int32, 0, len(d.Tracks))
	for _, t := range d.Tracks {
		ids = append(ids, t.TrackID)
		tns = append(tns, t.TrackNumber)
		dns = append(dns, t.DiscNumber)
	}
	_, err = q.db.Exec(ctx, `
		WITH a AS (
			UPDATE albums SET album_type = COALESCE($2, album_type), total_tracks = COALESCE($3, total_tracks),
				release_date_precision = COALESCE($4, release_date_precision), upc = COALESCE($5, upc),
				copyrights = $6::jsonb, details_fetched_at = now()
			WHERE id = $1 RETURNING id)
		UPDATE tracks t SET track_number = COALESCE(u.tn, t.track_number),
			disc_number = COALESCE(u.dn, t.disc_number)
		FROM a, unnest($7::text[], $8::int[], $9::int[]) AS u(id, tn, dn)
		WHERE t.id = u.id AND t.album_id = a.id
			AND ((u.tn IS NOT NULL AND t.track_number IS DISTINCT FROM u.tn)
				OR (u.dn IS NOT NULL AND t.disc_number IS DISTINCT FROM u.dn))`,
		id, nullIfEmpty(d.AlbumType), d.TotalTracks, nullIfEmpty(d.ReleaseDatePrecision),
		nullIfEmpty(d.UPC), string(cj), ids, tns, dns)
	if err != nil {
		return fmt.Errorf("set album details %s: %w", id, err)
	}
	return nil
}

// DeferAlbumDetails records a failed lookup as if it had been made age ago,
// so the album leaves the head of the queue and is retried once it is stale
// again. Stored details are kept.
func (q *Queries) DeferAlbumDetails(ctx context.Context, id string, age time.Duration) error {
	_, err := q.db.Exec(ctx, `
		UPDATE albums SET details_fetched_at = now() - make_interval(secs => $2::bigint)
		WHERE id = $1`, id, int64(age.Seconds()))
	if err != nil {
		return fmt.Errorf("defer album details %s: %w", id, err)
	}
	return nil
}
