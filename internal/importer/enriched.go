package importer

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// idBatch is how many ids go into one Directus `_in` filter (keeps URLs short).
const idBatch = 100

// EnrichedStats summarises an enriched-only import.
type EnrichedStats struct {
	FeaturesFound    int // enriched rows in Directus (after de-duplication)
	FeaturesImported int // written to the new DB
	FeaturesKept     int // skipped: already enriched in the new DB
	Orphans          int // enriched rows whose track no longer exists in Directus
	Tracks           int
	Albums           int
	Artists          int
}

// RunEnriched imports only enriched audio features and the catalog rows they
// depend on (tracks, their albums and artists). It never imports listening
// history or current playback.
//
// Duplicates are handled at every level: Directus rows are de-duplicated by
// Spotify ID before writing, catalog writes are upserts that keep existing
// non-empty fields, and features never overwrite a track the new database
// has already enriched.
func (im *Importer) RunEnriched(ctx context.Context) (EnrichedStats, error) {
	var st EnrichedStats
	start := time.Now()

	features, err := im.enrichedFeatures(ctx)
	if err != nil {
		return st, fmt.Errorf("load enriched features: %w", err)
	}
	st.FeaturesFound = len(features)
	im.log.Info("enriched features loaded", slog.Int("tracks", len(features)))

	tracks, err := fetchByIDs[dTrack](ctx, im, "tracks", "*,artists.*", keys(features), func(t dTrack) string { return t.ID })
	if err != nil {
		return st, fmt.Errorf("load tracks: %w", err)
	}
	albumIDs, artistIDs := map[string]struct{}{}, map[string]struct{}{}
	for _, t := range tracks {
		if id := relationID(t.AlbumID); id != "" {
			albumIDs[id] = struct{}{}
		}
		for _, id := range junctionIDs(t.Artists) {
			artistIDs[id] = struct{}{}
		}
	}
	albums, err := fetchByIDs[dAlbum](ctx, im, "albums", "id,name,release_date,cover_image,uri,external_url",
		keys(albumIDs), func(a dAlbum) string { return a.ID })
	if err != nil {
		return st, fmt.Errorf("load albums: %w", err)
	}
	artists, err := fetchByIDs[dArtist](ctx, im, "artists", "id,name,uri,external_url",
		keys(artistIDs), func(a dArtist) string { return a.ID })
	if err != nil {
		return st, fmt.Errorf("load artists: %w", err)
	}
	st.Tracks, st.Albums, st.Artists = len(tracks), len(albums), len(artists)
	st.Orphans = len(features) - countPresent(features, tracks)

	if im.dryRun {
		st.FeaturesImported = len(features) - st.Orphans
		im.log.Info("dry run: nothing written", slog.Duration("took", time.Since(start)))
		return st, nil
	}
	if err := im.writeCatalog(ctx, artists, albums, tracks); err != nil {
		return st, err
	}
	imported, kept, err := im.writeFeatures(ctx, features, tracks)
	st.FeaturesImported, st.FeaturesKept = imported, kept
	im.log.Info("enriched import written", slog.Duration("took", time.Since(start)))
	return st, err
}

// enrichedFeatures pages through enriched track_features, de-duplicated by
// track ID (keeping the most complete row).
func (im *Importer) enrichedFeatures(ctx context.Context) (map[string]dFeatures, error) {
	out := map[string]dFeatures{}
	q := url.Values{"filter[enriched][_eq]": {"true"}}
	err := pagesWith(ctx, im, "track_features", "*", "id", q, func(items []dFeatures) error {
		for _, f := range items {
			id := relationID(f.TrackID)
			if id == "" {
				continue
			}
			if cur, ok := out[id]; !ok || completeness(f) > completeness(cur) {
				out[id] = f
			}
		}
		return nil
	})
	return out, err
}

// completeness counts populated feature values (used to pick between
// duplicate legacy rows for the same track).
func completeness(f dFeatures) int {
	n := 0
	for _, v := range []*flexFloat{f.Acousticness, f.Danceability, f.Energy, f.Instrumentalness, f.Liveness,
		f.Loudness, f.Speechiness, f.Tempo, f.Valence} {
		if v != nil {
			n++
		}
	}
	if f.Key != nil {
		n++
	}
	if f.Mode != nil {
		n++
	}
	if len(parseGenres(f.Genres)) > 0 {
		n++
	}
	return n
}

// fetchByIDs loads rows of a collection whose ids are in ids, in batches,
// de-duplicated by id.
func fetchByIDs[T any](ctx context.Context, im *Importer, collection, fields string, ids []string, idOf func(T) string) (map[string]T, error) {
	out := make(map[string]T, len(ids))
	for i := 0; i < len(ids); i += idBatch {
		batch := ids[i:min(i+idBatch, len(ids))]
		q := url.Values{
			"filter[id][_in]": {strings.Join(batch, ",")},
			"limit":           {strconv.Itoa(len(batch))},
			"fields":          {fields},
		}
		var resp struct {
			Data []T `json:"data"`
		}
		if err := im.get(ctx, "/items/"+collection, q, &resp); err != nil {
			return nil, err
		}
		for _, row := range resp.Data {
			if id := idOf(row); id != "" {
				out[id] = row
			}
		}
	}
	return out, nil
}

// writeCatalog upserts artists, albums and tracks (in dependency order), one
// transaction per kind. Upserts keep existing non-empty fields.
func (im *Importer) writeCatalog(ctx context.Context, artists map[string]dArtist, albums map[string]dAlbum, tracks map[string]dTrack) error {
	err := im.store.InTx(ctx, func(q *store.Queries) error {
		for _, a := range artists {
			if err := q.UpsertArtist(ctx, domain.Artist{ID: a.ID, Name: a.Name, URI: str(a.URI), ExternalURL: str(a.ExternalURL)}); err != nil {
				return err
			}
		}
		for _, a := range albums {
			if err := q.UpsertAlbum(ctx, domain.Album{ID: a.ID, Name: a.Name, ReleaseDate: str(a.ReleaseDate),
				CoverImageURL: str(a.CoverImage), URI: str(a.URI), ExternalURL: str(a.ExternalURL)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("write artists/albums: %w", err)
	}
	err = im.store.InTx(ctx, func(q *store.Queries) error {
		for _, t := range tracks {
			albumID := relationID(t.AlbumID)
			if _, ok := albums[albumID]; !ok {
				albumID = "" // album missing upstream: don't reference it
			}
			var artistIDs []string
			for _, id := range junctionIDs(t.Artists) {
				if _, ok := artists[id]; ok {
					artistIDs = append(artistIDs, id)
				}
			}
			if err := q.UpsertTrack(ctx, domain.Track{
				ID: t.ID, Name: t.Name, URI: str(t.URI), ISRC: str(t.ISRC),
				DurationMS: int32(t.DurationMS), Explicit: t.Explicit != nil && *t.Explicit,
				ExternalURL: str(t.ExternalURL), AlbumID: albumID,
			}, artistIDs); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("write tracks: %w", err)
	}
	return nil
}

// writeFeatures stores features for tracks that exist, never overwriting a
// track the new database has already enriched.
func (im *Importer) writeFeatures(ctx context.Context, features map[string]dFeatures, tracks map[string]dTrack) (imported, kept int, err error) {
	err = im.store.InTx(ctx, func(q *store.Queries) error {
		for id, f := range features {
			t, ok := tracks[id]
			if !ok {
				continue
			}
			isrc := str(f.ISRC)
			if isrc == "" {
				isrc = str(t.ISRC)
			}
			genres := parseGenres(f.Genres)
			af := toAudioFeatures(f)
			found := af.Energy != nil || af.Tempo != nil || af.Danceability != nil || len(genres) > 0
			saved, err := q.ImportFeatures(ctx, id, isrc, found, af, genres, "soundcharts")
			if err != nil {
				return err
			}
			if saved {
				imported++
			} else {
				kept++
			}
		}
		return nil
	})
	return imported, kept, err
}

func toAudioFeatures(f dFeatures) domain.AudioFeatures {
	return domain.AudioFeatures{
		Acousticness: f.Acousticness.ptr(), Danceability: f.Danceability.ptr(),
		Energy: f.Energy.ptr(), Instrumentalness: f.Instrumentalness.ptr(),
		Key: f.Key.ptr32(), Liveness: f.Liveness.ptr(), Loudness: f.Loudness.ptr(),
		Mode: f.Mode.ptr32(), Speechiness: f.Speechiness.ptr(), Tempo: f.Tempo.ptr(),
		Valence: f.Valence.ptr(),
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func countPresent[A, B any](a map[string]A, b map[string]B) int {
	n := 0
	for k := range a {
		if _, ok := b[k]; ok {
			n++
		}
	}
	return n
}
