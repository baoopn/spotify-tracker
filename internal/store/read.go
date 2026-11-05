package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

const trackColumns = `
	t.id, t.name, COALESCE(t.uri, ''), COALESCE(t.isrc, ''), t.duration_ms, t.explicit,
	COALESCE(t.external_url, ''), COALESCE(t.album_id, ''),
	al.id, al.name, al.release_date, al.cover_image_url, al.uri, al.external_url`

// scanTrack scans trackColumns into a TrackView (artists not hydrated).
func scanTrack(dest *domain.TrackView, extra ...any) []any {
	return append([]any{
		&dest.ID, &dest.Name, &dest.URI, &dest.ISRC, &dest.DurationMS, &dest.Explicit,
		&dest.ExternalURL, &dest.AlbumID,
	}, extra...)
}

type albumScan struct {
	id, name, releaseDate, cover, uri, externalURL *string
}

func (a *albumScan) targets() []any {
	return []any{&a.id, &a.name, &a.releaseDate, &a.cover, &a.uri, &a.externalURL}
}

func (a *albumScan) album() *domain.Album {
	if a.id == nil {
		return nil
	}
	return &domain.Album{
		ID: *a.id, Name: deref(a.name), ReleaseDate: deref(a.releaseDate),
		CoverImageURL: deref(a.cover), URI: deref(a.uri), ExternalURL: deref(a.externalURL),
	}
}

// artistsFor loads credited artists for the given tracks, in credit order.
func (q *Queries) artistsFor(ctx context.Context, trackIDs []string) (map[string][]domain.Artist, error) {
	out := make(map[string][]domain.Artist, len(trackIDs))
	if len(trackIDs) == 0 {
		return out, nil
	}
	rows, err := q.db.Query(ctx, `
		SELECT ta.track_id, a.id, a.name, COALESCE(a.uri, ''), COALESCE(a.external_url, ''), COALESCE(a.image_url, '')
		FROM track_artists ta JOIN artists a ON a.id = ta.artist_id
		WHERE ta.track_id = ANY($1)
		ORDER BY ta.track_id, ta.position`, trackIDs)
	if err != nil {
		return nil, fmt.Errorf("load artists: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tid string
		var a domain.Artist
		if err := rows.Scan(&tid, &a.ID, &a.Name, &a.URI, &a.ExternalURL, &a.ImageURL); err != nil {
			return nil, err
		}
		out[tid] = append(out[tid], a)
	}
	return out, rows.Err()
}

func (q *Queries) hydrate(ctx context.Context, views ...*domain.TrackView) error {
	ids := make([]string, 0, len(views))
	for _, v := range views {
		ids = append(ids, v.ID)
	}
	m, err := q.artistsFor(ctx, ids)
	if err != nil {
		return err
	}
	for _, v := range views {
		v.Artists = m[v.ID]
		if v.Artists == nil {
			v.Artists = []domain.Artist{}
		}
	}
	return nil
}

// NowPlaying returns the current playback state, or ErrNotFound if nothing was
// ever recorded.
func (q *Queries) NowPlaying(ctx context.Context) (domain.NowPlaying, error) {
	var np domain.NowPlaying
	var tv domain.TrackView
	var tid *string
	var al albumScan
	err := q.db.QueryRow(ctx, `
		SELECT cp.is_playing, cp.progress_ms, cp.fetched_at, cp.track_id,
			COALESCE(t.id, ''), COALESCE(t.name, ''), COALESCE(t.uri, ''), COALESCE(t.isrc, ''),
			COALESCE(t.duration_ms, 0), COALESCE(t.explicit, false), COALESCE(t.external_url, ''),
			COALESCE(t.album_id, ''),
			al.id, al.name, al.release_date, al.cover_image_url, al.uri, al.external_url
		FROM current_playback cp
		LEFT JOIN tracks t ON t.id = cp.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE cp.id`).Scan(append(append([]any{&np.IsPlaying, &np.ProgressMS, &np.FetchedAt, &tid},
		scanTrack(&tv)...), al.targets()...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return np, domain.ErrNotFound
	}
	if err != nil {
		return np, fmt.Errorf("now playing: %w", err)
	}
	if tid != nil {
		tv.Album = al.album()
		if err := q.hydrate(ctx, &tv); err != nil {
			return np, err
		}
		np.Track = &tv
	}
	return np, nil
}

// History returns history rows played strictly before `before` (zero = now),
// newest first.
func (q *Queries) History(ctx context.Context, limit int, before time.Time) ([]domain.HistoryEntry, error) {
	if before.IsZero() {
		before = time.Now().Add(time.Minute)
	}
	rows, err := q.db.Query(ctx, `
		SELECT h.id::text, h.played_at, h.updated_at, h.replay_count, `+trackColumns+`
		FROM listening_history h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE h.played_at < $1
		ORDER BY h.played_at DESC
		LIMIT $2`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	defer rows.Close()
	var out []domain.HistoryEntry
	for rows.Next() {
		var e domain.HistoryEntry
		var al albumScan
		if err := rows.Scan(append(append([]any{&e.ID, &e.PlayedAt, &e.UpdatedAt, &e.ReplayCount},
			scanTrack(&e.Track)...), al.targets()...)...); err != nil {
			return nil, err
		}
		e.Track.Album = al.album()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	views := make([]*domain.TrackView, len(out))
	for i := range out {
		views[i] = &out[i].Track
	}
	if err := q.hydrate(ctx, views...); err != nil {
		return nil, err
	}
	if out == nil {
		out = []domain.HistoryEntry{}
	}
	return out, nil
}

// TopTracks ranks tracks by play count within the range.
func (q *Queries) TopTracks(ctx context.Context, r domain.Range, limit int) ([]domain.TopTrack, error) {
	rows, err := q.db.Query(ctx, `
		WITH ranked AS (
			SELECT track_id, SUM(replay_count)::bigint AS plays, MAX(played_at) AS last_played
			FROM listening_history
			WHERE played_at >= $1
			GROUP BY track_id
			ORDER BY plays DESC, last_played DESC
			LIMIT $2
		)
		SELECT r.plays, r.last_played, `+trackColumns+`
		FROM ranked r
		JOIN tracks t ON t.id = r.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		ORDER BY r.plays DESC, r.last_played DESC`, r.Since, limit)
	if err != nil {
		return nil, fmt.Errorf("top tracks: %w", err)
	}
	defer rows.Close()
	out := []domain.TopTrack{}
	for rows.Next() {
		var tt domain.TopTrack
		var al albumScan
		if err := rows.Scan(append(append([]any{&tt.Plays, &tt.LastPlayed},
			scanTrack(&tt.Track)...), al.targets()...)...); err != nil {
			return nil, err
		}
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

// TopArtists ranks artists by play count within the range.
func (q *Queries) TopArtists(ctx context.Context, r domain.Range, limit int) ([]domain.TopArtist, error) {
	rows, err := q.db.Query(ctx, `
		SELECT a.id, a.name, COALESCE(a.uri, ''), COALESCE(a.external_url, ''), COALESCE(a.image_url, ''),
			SUM(h.replay_count)::bigint AS plays, MAX(h.played_at) AS last_played
		FROM listening_history h
		JOIN track_artists ta ON ta.track_id = h.track_id
		JOIN artists a ON a.id = ta.artist_id
		WHERE h.played_at >= $1
		GROUP BY a.id
		ORDER BY plays DESC, last_played DESC
		LIMIT $2`, r.Since, limit)
	if err != nil {
		return nil, fmt.Errorf("top artists: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TopArtist, error) {
		var ta domain.TopArtist
		err := row.Scan(&ta.Artist.ID, &ta.Artist.Name, &ta.Artist.URI, &ta.Artist.ExternalURL, &ta.Artist.ImageURL,
			&ta.Plays, &ta.LastPlayed)
		return ta, err
	})
}

// Genres aggregates plays per root genre (and sub-genre) within the range.
// Root plays come from a separate query so a track with several sub-genres
// is counted once for its root genre.
func (q *Queries) Genres(ctx context.Context, r domain.Range, limit int) ([]domain.GenreStat, error) {
	subs, err := q.genreSubPlays(ctx, r)
	if err != nil {
		return nil, err
	}
	out, err := q.genreRootPlays(ctx, r)
	if err != nil {
		return nil, err
	}
	var total int64
	for i := range out {
		total += out[i].Plays
		out[i].Sub = sortedSubGenres(subs[out[i].Genre])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plays == out[j].Plays {
			return out[i].Genre < out[j].Genre
		}
		return out[i].Plays > out[j].Plays
	})
	if total > 0 {
		for i := range out {
			out[i].Share = float64(out[i].Plays) / float64(total)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

const genrePlaysCTE = `
	WITH plays AS (
		SELECT track_id, SUM(replay_count)::bigint AS plays
		FROM listening_history WHERE played_at >= $1 GROUP BY track_id
	)`

// genreSubPlays returns plays per sub-genre, keyed by root genre.
func (q *Queries) genreSubPlays(ctx context.Context, r domain.Range) (map[string]map[string]int64, error) {
	rows, err := q.db.Query(ctx, genrePlaysCTE+`
		SELECT g->>'root' AS root, s.sub, SUM(p.plays)::bigint
		FROM plays p
		JOIN track_features tf ON tf.track_id = p.track_id AND tf.genres IS NOT NULL
		CROSS JOIN LATERAL jsonb_array_elements(tf.genres) AS g
		CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(g->'sub', '[]'::jsonb)) AS s(sub)
		WHERE g->>'root' IS NOT NULL AND g->>'root' <> '' AND s.sub <> ''
		GROUP BY root, s.sub`, r.Since)
	if err != nil {
		return nil, fmt.Errorf("genre subs: %w", err)
	}
	defer rows.Close()
	out := map[string]map[string]int64{}
	for rows.Next() {
		var root, sub string
		var n int64
		if err := rows.Scan(&root, &sub, &n); err != nil {
			return nil, err
		}
		if out[root] == nil {
			out[root] = map[string]int64{}
		}
		out[root][sub] += n
	}
	return out, rows.Err()
}

// genreRootPlays returns plays per root genre (unsorted, no sub-genres).
func (q *Queries) genreRootPlays(ctx context.Context, r domain.Range) ([]domain.GenreStat, error) {
	rows, err := q.db.Query(ctx, genrePlaysCTE+`
		SELECT g.root, SUM(p.plays)::bigint
		FROM plays p
		JOIN track_features tf ON tf.track_id = p.track_id AND tf.genres IS NOT NULL
		CROSS JOIN LATERAL (
			SELECT DISTINCT e->>'root' AS root FROM jsonb_array_elements(tf.genres) e
		) g
		WHERE g.root IS NOT NULL AND g.root <> ''
		GROUP BY g.root`, r.Since)
	if err != nil {
		return nil, fmt.Errorf("genre roots: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.GenreStat, error) {
		var gs domain.GenreStat
		err := row.Scan(&gs.Genre, &gs.Plays)
		return gs, err
	})
	if out == nil {
		out = []domain.GenreStat{}
	}
	return out, err
}

// sortedSubGenres orders sub-genres by plays (desc), then name.
func sortedSubGenres(m map[string]int64) []domain.SubGenreStat {
	out := make([]domain.SubGenreStat, 0, len(m))
	for name, n := range m {
		out = append(out, domain.SubGenreStat{Genre: name, Plays: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plays == out[j].Plays {
			return out[i].Genre < out[j].Genre
		}
		return out[i].Plays > out[j].Plays
	})
	return out
}

// AudioProfile returns play-weighted averages of audio features.
func (q *Queries) AudioProfile(ctx context.Context, r domain.Range) (domain.AudioProfile, error) {
	var p domain.AudioProfile
	err := q.db.QueryRow(ctx, `
		WITH plays AS (
			SELECT track_id, SUM(replay_count)::bigint AS plays
			FROM listening_history WHERE played_at >= $1 GROUP BY track_id
		)
		SELECT COUNT(*), COALESCE(SUM(p.plays), 0)::bigint,
			SUM(tf.acousticness * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.acousticness IS NOT NULL), 0),
			SUM(tf.danceability * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.danceability IS NOT NULL), 0),
			SUM(tf.energy * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.energy IS NOT NULL), 0),
			SUM(tf.instrumentalness * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.instrumentalness IS NOT NULL), 0),
			SUM(tf.liveness * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.liveness IS NOT NULL), 0),
			SUM(tf.loudness * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.loudness IS NOT NULL), 0),
			SUM(tf.speechiness * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.speechiness IS NOT NULL), 0),
			SUM(tf.tempo * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.tempo IS NOT NULL), 0),
			SUM(tf.valence * p.plays) / NULLIF(SUM(p.plays) FILTER (WHERE tf.valence IS NOT NULL), 0)
		FROM plays p
		JOIN track_features tf ON tf.track_id = p.track_id AND tf.status = 'enriched'`, r.Since).
		Scan(&p.SampleTracks, &p.SamplePlays, &p.Acousticness, &p.Danceability, &p.Energy,
			&p.Instrumentalness, &p.Liveness, &p.Loudness, &p.Speechiness, &p.Tempo, &p.Valence)
	if err != nil {
		return p, fmt.Errorf("audio profile: %w", err)
	}
	return p, nil
}

// Summary returns overall counters within the range.
func (q *Queries) Summary(ctx context.Context, r domain.Range) (domain.Summary, error) {
	var s domain.Summary
	err := q.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(h.replay_count), 0)::bigint,
			COUNT(DISTINCT h.track_id),
			(SELECT COUNT(DISTINCT ta.artist_id) FROM listening_history h2
				JOIN track_artists ta ON ta.track_id = h2.track_id WHERE h2.played_at >= $1),
			COALESCE(SUM(COALESCE(h.duration_ms, t.duration_ms)::bigint * h.replay_count), 0)::bigint,
			MIN(h.played_at), MAX(h.played_at),
			(SELECT COUNT(*) FROM track_features WHERE status = 'enriched')
		FROM listening_history h JOIN tracks t ON t.id = h.track_id
		WHERE h.played_at >= $1`, r.Since).
		Scan(&s.TotalPlays, &s.UniqueTracks, &s.UniqueArtists, &s.ListeningMS,
			&s.FirstPlayedAt, &s.LastPlayedAt, &s.EnrichedTracks)
	if err != nil {
		return s, fmt.Errorf("summary: %w", err)
	}
	return s, nil
}

// TrackDetail returns a track with features and play counters.
func (q *Queries) TrackDetail(ctx context.Context, id string) (domain.TrackDetail, error) {
	var d domain.TrackDetail
	var al albumScan
	err := q.db.QueryRow(ctx, `SELECT `+trackColumns+`,
			(SELECT COALESCE(SUM(replay_count), 0)::bigint FROM listening_history WHERE track_id = t.id),
			(SELECT MAX(played_at) FROM listening_history WHERE track_id = t.id)
		FROM tracks t LEFT JOIN albums al ON al.id = t.album_id
		WHERE t.id = $1`, id).
		Scan(append(append(scanTrack(&d.TrackView), al.targets()...), &d.Plays, &d.LastPlayed)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.ErrNotFound
	}
	if err != nil {
		return d, fmt.Errorf("track detail: %w", err)
	}
	d.Album = al.album()
	if err := q.hydrate(ctx, &d.TrackView); err != nil {
		return d, err
	}

	var tf domain.TrackFeatures
	var genres []byte
	var source *string
	f := &tf.Features
	err = q.db.QueryRow(ctx, `
		SELECT status, source, acousticness, danceability, energy, instrumentalness, key,
			liveness, loudness, mode, speechiness, tempo, time_signature, valence, genres, enriched_at
		FROM track_features WHERE track_id = $1`, id).
		Scan(&tf.Status, &source, &f.Acousticness, &f.Danceability, &f.Energy, &f.Instrumentalness,
			&f.Key, &f.Liveness, &f.Loudness, &f.Mode, &f.Speechiness, &f.Tempo, &f.TimeSignature,
			&f.Valence, &genres, &tf.EnrichedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return d, nil
	case err != nil:
		return d, fmt.Errorf("track features: %w", err)
	}
	tf.Source = deref(source)
	tf.Genres = []domain.Genre{}
	if len(genres) > 0 {
		if err := json.Unmarshal(genres, &tf.Genres); err != nil {
			return d, fmt.Errorf("decode genres: %w", err)
		}
	}
	d.Features = &tf
	return d, nil
}
