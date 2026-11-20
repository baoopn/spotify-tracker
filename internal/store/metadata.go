package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// detailExtras adds the detail-only metadata (album UPC, copyrights and
// lookup time, artist genres) to a hydrated track. List endpoints skip it.
func (q *Queries) detailExtras(ctx context.Context, v *domain.TrackView) error {
	if err := q.albumExtras(ctx, v.Album); err != nil {
		return err
	}
	return q.artistGenres(ctx, v.Artists)
}

// hideAdminOnly drops the fields only the admin detail shows (the album
// lookup time) from a public track detail.
func hideAdminOnly(v *domain.TrackView) {
	if v.Album != nil {
		v.Album.DetailsFetchedAt = nil
	}
}

// albumExtras loads an album's UPC, copyrights and details lookup time.
func (q *Queries) albumExtras(ctx context.Context, al *domain.Album) error {
	if al == nil || al.ID == "" {
		return nil
	}
	var upc *string
	var copyrights []byte
	err := q.db.QueryRow(ctx, `
		SELECT upc, copyrights, details_fetched_at FROM albums WHERE id = $1`, al.ID).
		Scan(&upc, &copyrights, &al.DetailsFetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("album extras %s: %w", al.ID, err)
	}
	al.UPC = deref(upc)
	if len(copyrights) > 0 {
		var cs []domain.Copyright
		if err := json.Unmarshal(copyrights, &cs); err != nil {
			return fmt.Errorf("decode copyrights %s: %w", al.ID, err)
		}
		if len(cs) > 0 {
			al.Copyrights = cs
		}
	}
	return nil
}

// artistGenres fills in the Spotify genres of the given artists. Artists with
// no known genres keep a nil list (omitted from JSON).
func (q *Queries) artistGenres(ctx context.Context, artists []domain.Artist) error {
	if len(artists) == 0 {
		return nil
	}
	ids := make([]string, len(artists))
	for i, a := range artists {
		ids[i] = a.ID
	}
	rows, err := q.db.Query(ctx, `
		SELECT id, genres FROM artists
		WHERE id = ANY($1) AND cardinality(genres) > 0`, ids)
	if err != nil {
		return fmt.Errorf("artist genres: %w", err)
	}
	defer rows.Close()
	genres := map[string][]string{}
	for rows.Next() {
		var id string
		var g []string
		if err := rows.Scan(&id, &g); err != nil {
			return err
		}
		genres[id] = g
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range artists {
		if g := genres[artists[i].ID]; len(g) > 0 {
			artists[i].Genres = g
		}
	}
	return nil
}
