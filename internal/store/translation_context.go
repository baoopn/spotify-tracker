package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// TranslationContext reads what the database knows about a track for its
// translation request: credited artists, album, release date, artist genres
// (Spotify), enrichment genres (only once enriched), explicit flag, duration
// and the source language of an earlier translation. A missing track gives
// an empty context and no error. It reads only; the claim is not involved.
func (q *Queries) TranslationContext(ctx context.Context, trackID string) (domain.TranslationContext, error) {
	var c domain.TranslationContext
	var genres []byte
	err := q.db.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT array_agg(ar.name ORDER BY ta.position, ar.name)
				FROM track_artists ta JOIN artists ar ON ar.id = ta.artist_id
				WHERE ta.track_id = t.id), '{}'),
			COALESCE(al.name, ''), COALESCE(al.album_type, ''), COALESCE(al.release_date, ''),
			COALESCE((SELECT array_agg(x.g ORDER BY x.p, x.g) FROM (
				SELECT g, MIN(ta.position) AS p
				FROM track_artists ta JOIN artists ar ON ar.id = ta.artist_id
				CROSS JOIN LATERAL unnest(ar.genres) AS g
				WHERE ta.track_id = t.id GROUP BY g) x), '{}'),
			tf.genres, t.explicit, t.duration_ms, COALESCE(tr.source_language, '')
		FROM tracks t
		LEFT JOIN albums al ON al.id = t.album_id
		LEFT JOIN track_features tf ON tf.track_id = t.id AND tf.status = 'enriched'
		LEFT JOIN lyrics_translations tr ON tr.track_id = t.id
		WHERE t.id = $1`, trackID).Scan(&c.Artists, &c.Album, &c.AlbumType, &c.ReleaseDate,
		&c.ArtistGenres, &genres, &c.Explicit, &c.DurationMs, &c.Language)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TranslationContext{}, nil
	}
	if err != nil {
		return domain.TranslationContext{}, fmt.Errorf("translation context %s: %w", trackID, err)
	}
	// Bad enrichment JSON only loses the genres, not the translation.
	if len(genres) > 0 && json.Unmarshal(genres, &c.Genres) != nil {
		c.Genres = nil
	}
	return c, nil
}
