package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ArtistsNeedingProfile returns up to limit artist IDs whose profile (image
// and genres) has never been looked up, or was last looked up before
// staleBefore. Artists never looked up come first, then artists whose genres
// were never looked up, most recently played first.
func (q *Queries) ArtistsNeedingProfile(ctx context.Context, staleBefore time.Time, limit int) ([]string, error) {
	rows, err := q.db.Query(ctx, `
		SELECT a.id FROM artists a
		LEFT JOIN LATERAL (
			SELECT MAX(h.played_at) AS last_played
			FROM track_artists ta JOIN listening_history h ON h.track_id = ta.track_id
			WHERE ta.artist_id = a.id
		) p ON true
		WHERE a.image_fetched_at IS NULL OR a.genres_fetched_at IS NULL OR a.image_fetched_at < $1
		ORDER BY (a.image_fetched_at IS NULL) DESC, (a.genres_fetched_at IS NULL) DESC,
			p.last_played DESC NULLS LAST, a.image_fetched_at, a.id
		LIMIT $2`, staleBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("artists needing profile: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// SetArtistProfile records a profile lookup. An empty imageURL keeps any
// image stored earlier, and nil genres (a refused lookup) keep the stored
// genres; an empty non-nil list stores '{}' (Spotify lists none).
func (q *Queries) SetArtistProfile(ctx context.Context, id, imageURL string, genres []string) error {
	var g any
	if genres != nil {
		g = genres
	}
	_, err := q.db.Exec(ctx, `
		UPDATE artists SET image_url = COALESCE($2, image_url), genres = COALESCE($3, genres),
			image_fetched_at = now(), genres_fetched_at = now()
		WHERE id = $1`, id, nullIfEmpty(imageURL), g)
	if err != nil {
		return fmt.Errorf("set artist profile %s: %w", id, err)
	}
	return nil
}
