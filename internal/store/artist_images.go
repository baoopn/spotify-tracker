package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ArtistsNeedingImage returns up to limit artist IDs whose profile image has
// never been looked up, or was last looked up before staleBefore. Artists
// never looked up come first, most recently played first.
func (q *Queries) ArtistsNeedingImage(ctx context.Context, staleBefore time.Time, limit int) ([]string, error) {
	rows, err := q.db.Query(ctx, `
		SELECT a.id FROM artists a
		LEFT JOIN LATERAL (
			SELECT MAX(h.played_at) AS last_played
			FROM track_artists ta JOIN listening_history h ON h.track_id = ta.track_id
			WHERE ta.artist_id = a.id
		) p ON true
		WHERE a.image_fetched_at IS NULL OR a.image_fetched_at < $1
		ORDER BY a.image_fetched_at NULLS FIRST, p.last_played DESC NULLS LAST, a.id
		LIMIT $2`, staleBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("artists needing image: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// SetArtistImage records an image lookup. An empty url keeps any image
// stored earlier (a refused or image-less lookup must not wipe it).
func (q *Queries) SetArtistImage(ctx context.Context, id, url string) error {
	_, err := q.db.Exec(ctx, `
		UPDATE artists SET image_url = COALESCE($2, image_url), image_fetched_at = now()
		WHERE id = $1`, id, nullIfEmpty(url))
	if err != nil {
		return fmt.Errorf("set artist image %s: %w", id, err)
	}
	return nil
}
