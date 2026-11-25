package store_test

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestPGTranslationContext(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	exec(t, pool, `TRUNCATE track_features, albums CASCADE`)
	seed(t, pool, "full", time.Minute)
	exec(t, pool, `INSERT INTO albums (id, name, album_type, release_date) VALUES ('al', 'The Album', 'single', '2018-05-12')`)
	exec(t, pool, `UPDATE tracks SET album_id = 'al', explicit = true, duration_ms = 245900 WHERE id = 'full'`)
	exec(t, pool, `UPDATE artists SET genres = '{v-pop,rap}' WHERE id = 'ar'`)
	exec(t, pool, `INSERT INTO artists (id, name, genres) VALUES ('feat', 'Guest', '{rap,trap}')`)
	exec(t, pool, `INSERT INTO track_artists (track_id, artist_id, position) VALUES ('full', 'feat', 1)`)
	exec(t, pool, `INSERT INTO track_features (track_id, status, genres)
		VALUES ('full', 'enriched', '[{"root":"pop","sub":["dance pop"]}]')`)
	exec(t, pool, `INSERT INTO lyrics_translations (track_id, status, source_hash, source_language)
		VALUES ('full', 'done', 'h', 'vi')`)

	c, err := st.TranslationContext(ctx, "full")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Artists, []string{"Artist", "Guest"}) || c.Album != "The Album" || c.AlbumType != "single" ||
		c.ReleaseDate != "2018-05-12" || !c.Explicit || c.DurationMs != 245900 || c.Language != "vi" {
		t.Errorf("context = %+v", c)
	}
	// Artist genres in credit order, once each.
	if !slices.Equal(c.ArtistGenres, []string{"rap", "v-pop", "trap"}) {
		t.Errorf("artist genres = %v", c.ArtistGenres)
	}
	if len(c.Genres) != 1 || c.Genres[0].Root != "pop" || !slices.Equal(c.Genres[0].Sub, []string{"dance pop"}) {
		t.Errorf("genres = %+v", c.Genres)
	}

	// A bare track: everything unknown is empty; pending features are not used.
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms) VALUES ('bare', 'x', 0)`)
	exec(t, pool, `INSERT INTO track_features (track_id, status, genres) VALUES ('bare', 'pending', '[{"root":"x"}]')`)
	c, err = st.TranslationContext(ctx, "bare")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Artists) != 0 || c.Album != "" || len(c.ArtistGenres) != 0 || c.Genres != nil || c.Explicit || c.Language != "" {
		t.Errorf("bare context = %+v", c)
	}
	if c, err = st.TranslationContext(ctx, "missing"); err != nil || c.Album != "" {
		t.Errorf("missing = %+v, %v", c, err)
	}
}
