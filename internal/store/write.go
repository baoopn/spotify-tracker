package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// UpsertArtist inserts or updates an artist.
func (q *Queries) UpsertArtist(ctx context.Context, a domain.Artist) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO artists (id, name, uri, external_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			uri = COALESCE(EXCLUDED.uri, artists.uri),
			external_url = COALESCE(EXCLUDED.external_url, artists.external_url)`,
		a.ID, a.Name, nullIfEmpty(a.URI), nullIfEmpty(a.ExternalURL))
	if err != nil {
		return fmt.Errorf("upsert artist %s: %w", a.ID, err)
	}
	return nil
}

// UpsertAlbum inserts or updates an album.
func (q *Queries) UpsertAlbum(ctx context.Context, a domain.Album) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO albums (id, name, release_date, cover_image_url, uri, external_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			release_date = COALESCE(EXCLUDED.release_date, albums.release_date),
			cover_image_url = COALESCE(EXCLUDED.cover_image_url, albums.cover_image_url),
			uri = COALESCE(EXCLUDED.uri, albums.uri),
			external_url = COALESCE(EXCLUDED.external_url, albums.external_url)`,
		a.ID, a.Name, nullIfEmpty(a.ReleaseDate), nullIfEmpty(a.CoverImageURL),
		nullIfEmpty(a.URI), nullIfEmpty(a.ExternalURL))
	if err != nil {
		return fmt.Errorf("upsert album %s: %w", a.ID, err)
	}
	return nil
}

// UpsertTrack inserts or updates a track and replaces its artist credits.
// Referenced artists and album must already exist.
func (q *Queries) UpsertTrack(ctx context.Context, t domain.Track, artistIDs []string) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO tracks (id, name, uri, isrc, duration_ms, explicit, external_url, album_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			uri = COALESCE(EXCLUDED.uri, tracks.uri),
			isrc = COALESCE(EXCLUDED.isrc, tracks.isrc),
			duration_ms = EXCLUDED.duration_ms,
			explicit = EXCLUDED.explicit,
			external_url = COALESCE(EXCLUDED.external_url, tracks.external_url),
			album_id = COALESCE(EXCLUDED.album_id, tracks.album_id)`,
		t.ID, t.Name, nullIfEmpty(t.URI), nullIfEmpty(t.ISRC), t.DurationMS, t.Explicit,
		nullIfEmpty(t.ExternalURL), nullIfEmpty(t.AlbumID))
	if err != nil {
		return fmt.Errorf("upsert track %s: %w", t.ID, err)
	}
	if len(artistIDs) == 0 {
		return nil
	}
	if _, err := q.db.Exec(ctx,
		`DELETE FROM track_artists WHERE track_id = $1 AND NOT (artist_id = ANY($2))`,
		t.ID, artistIDs); err != nil {
		return fmt.Errorf("prune track artists %s: %w", t.ID, err)
	}
	batch := &pgx.Batch{}
	for i, id := range artistIDs {
		batch.Queue(`
			INSERT INTO track_artists (track_id, artist_id, position) VALUES ($1, $2, $3)
			ON CONFLICT (track_id, artist_id) DO UPDATE SET position = EXCLUDED.position`,
			t.ID, id, i)
	}
	if err := q.db.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("link track artists %s: %w", t.ID, err)
	}
	return nil
}

// EnsureTrackFeatures creates a pending enrichment row for a track, or fills
// in a missing ISRC on an existing one. It returns true when the track should
// be (re)queued for enrichment: the row is new, or it just gained an ISRC.
func (q *Queries) EnsureTrackFeatures(ctx context.Context, trackID, isrc string) (bool, error) {
	tag, err := q.db.Exec(ctx, `
		INSERT INTO track_features (track_id, isrc) VALUES ($1, $2)
		ON CONFLICT (track_id) DO UPDATE SET isrc = EXCLUDED.isrc
		WHERE track_features.isrc IS NULL AND EXCLUDED.isrc IS NOT NULL
			AND track_features.status = 'pending'`,
		trackID, nullIfEmpty(isrc))
	if err != nil {
		return false, fmt.Errorf("ensure track features %s: %w", trackID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// SetCurrentPlayback upserts the singleton playback row.
func (q *Queries) SetCurrentPlayback(ctx context.Context, trackID string, isPlaying bool, progressMS *int32, fetchedAt time.Time) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO current_playback (id, track_id, is_playing, progress_ms, fetched_at)
		VALUES (true, $1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			track_id = EXCLUDED.track_id,
			is_playing = EXCLUDED.is_playing,
			progress_ms = EXCLUDED.progress_ms,
			fetched_at = EXCLUDED.fetched_at`,
		nullIfEmpty(trackID), isPlaying, progressMS, fetchedAt)
	if err != nil {
		return fmt.Errorf("set current playback: %w", err)
	}
	return nil
}

// MarkNotPlaying flags the current playback as stopped, keeping the last track.
// It returns true when the state changed.
func (q *Queries) MarkNotPlaying(ctx context.Context, fetchedAt time.Time) (bool, error) {
	tag, err := q.db.Exec(ctx, `
		UPDATE current_playback SET is_playing = false, fetched_at = $1
		WHERE id AND is_playing`, fetchedAt)
	if err != nil {
		return false, fmt.Errorf("mark not playing: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// LatestHistory returns the id and track id of the most recent history row.
func (q *Queries) LatestHistory(ctx context.Context) (id, trackID string, err error) {
	err = q.db.QueryRow(ctx, `
		SELECT id::text, track_id FROM listening_history
		ORDER BY played_at DESC LIMIT 1`).Scan(&id, &trackID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", domain.ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("latest history: %w", err)
	}
	return id, trackID, nil
}

// IncrementReplay bumps the replay counter of a history row.
func (q *Queries) IncrementReplay(ctx context.Context, id string, at time.Time) error {
	_, err := q.db.Exec(ctx, `
		UPDATE listening_history SET replay_count = replay_count + 1, updated_at = $2
		WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("increment replay: %w", err)
	}
	return nil
}

// InsertHistory appends a listening history row. Duplicate (track, played_at)
// pairs are ignored; the return value reports whether a row was created.
// pc (optional) records what the track was played from.
func (q *Queries) InsertHistory(ctx context.Context, trackID string, playedAt, updatedAt time.Time, durationMS int32, replayCount int32, pc *domain.PlaybackContext) (bool, error) {
	if replayCount < 1 {
		replayCount = 1
	}
	var ctxType, ctxID *string
	if pc != nil && pc.ID() != "" {
		t, id := pc.Type, pc.ID()
		ctxType, ctxID = &t, &id
	}
	tag, err := q.db.Exec(ctx, `
		INSERT INTO listening_history (track_id, played_at, updated_at, duration_ms, replay_count, context_type, context_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (track_id, played_at) DO NOTHING`,
		trackID, playedAt, updatedAt, durationMS, replayCount, ctxType, ctxID)
	if err != nil {
		return false, fmt.Errorf("insert history: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListPending returns tracks due for enrichment, oldest first.
func (q *Queries) ListPending(ctx context.Context, limit int) ([]domain.PendingTrack, error) {
	rows, err := q.db.Query(ctx, `
		SELECT tf.track_id, COALESCE(tf.isrc, t.isrc, ''), tf.attempts
		FROM track_features tf
		JOIN tracks t ON t.id = tf.track_id
		WHERE tf.status = 'pending' AND tf.next_attempt_at <= now()
		ORDER BY tf.next_attempt_at
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.PendingTrack, error) {
		var p domain.PendingTrack
		err := r.Scan(&p.TrackID, &p.ISRC, &p.Attempts)
		return p, err
	})
}

// SaveFeatures stores an enrichment result. found=false marks the track as
// unavailable at the provider.
func (q *Queries) SaveFeatures(ctx context.Context, trackID string, found bool, f domain.AudioFeatures, genres []domain.Genre, source string) error {
	status := domain.EnrichmentEnriched
	if !found {
		status = domain.EnrichmentUnavailable
		f = domain.AudioFeatures{}
		genres = nil
	}
	var genresJSON []byte
	if genres != nil {
		b, err := json.Marshal(genres)
		if err != nil {
			return fmt.Errorf("marshal genres: %w", err)
		}
		genresJSON = b
	}
	tag, err := q.db.Exec(ctx, `
		INSERT INTO track_features (
			track_id, status, source, acousticness, danceability, energy, instrumentalness,
			key, liveness, loudness, mode, speechiness, tempo, time_signature, valence,
			genres, last_error, enriched_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NULL, now())
		ON CONFLICT (track_id) DO UPDATE SET
			status = EXCLUDED.status, source = EXCLUDED.source,
			acousticness = EXCLUDED.acousticness, danceability = EXCLUDED.danceability,
			energy = EXCLUDED.energy, instrumentalness = EXCLUDED.instrumentalness,
			key = EXCLUDED.key, liveness = EXCLUDED.liveness, loudness = EXCLUDED.loudness,
			mode = EXCLUDED.mode, speechiness = EXCLUDED.speechiness, tempo = EXCLUDED.tempo,
			time_signature = EXCLUDED.time_signature, valence = EXCLUDED.valence,
			genres = EXCLUDED.genres, last_error = NULL, postponed_since = NULL, enriched_at = now()`,
		trackID, status, nullIfEmpty(source),
		f.Acousticness, f.Danceability, f.Energy, f.Instrumentalness,
		f.Key, f.Liveness, f.Loudness, f.Mode, f.Speechiness, f.Tempo, f.TimeSignature, f.Valence,
		genresJSON)
	if err != nil {
		var fk interface{ SQLState() string }
		if errors.As(err, &fk) && fk.SQLState() == "23503" {
			return domain.ErrNotFound
		}
		return fmt.Errorf("save features %s: %w", trackID, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// backoffSQL is Backoff(n) in SQL, for n an SQL expression (in seconds).
func backoffSQL(n string) string {
	return "LEAST(60 * power(2, LEAST(GREATEST(" + n + ", 1), 12) - 1), 86400)"
}

// RecordFailure increments the attempt counter and schedules a retry with
// exponential backoff (1m, 2m, 4m … capped at 24h). After maxAttempts the
// track is marked failed. It returns true when attempts are exhausted. Only
// a pending row changes (one UPDATE), so a result saved meanwhile is never
// overwritten; then it returns false, nil.
func (q *Queries) RecordFailure(ctx context.Context, trackID, msg string, maxAttempts int) (bool, error) {
	var attempts int
	err := q.db.QueryRow(ctx, `
		UPDATE track_features SET attempts = attempts + 1, last_error = left($2, 2000), postponed_since = NULL,
			status = CASE WHEN attempts + 1 >= $3 THEN 'failed' ELSE status END,
			next_attempt_at = CASE WHEN attempts + 1 >= $3 THEN next_attempt_at
				ELSE now() + make_interval(secs => `+backoffSQL("attempts + 1")+`) END
		WHERE track_id = $1 AND status = 'pending' RETURNING attempts`, trackID, msg, maxAttempts).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, q.featuresExist(ctx, trackID)
	}
	if err != nil {
		return false, fmt.Errorf("record failure %s: %w", trackID, err)
	}
	return attempts >= maxAttempts, nil
}

// PostponeMax is how long transient failures may postpone a track without
// counting attempts.
const PostponeMax = 24 * time.Hour

// PostponeEnrichment schedules the next try of a pending track after a
// transient provider failure (rate limit) without counting an attempt: after
// retryAfter when the provider sent one (capped at 24h), else the normal
// backoff for the attempts so far. One UPDATE, pending rows only. It returns
// false when it did not postpone: the track is not pending, or it has been
// postponed for PostponeMax already (the caller then counts the failure).
func (q *Queries) PostponeEnrichment(ctx context.Context, trackID, msg string, retryAfter time.Duration) (bool, error) {
	tag, err := q.db.Exec(ctx, `
		UPDATE track_features SET last_error = left($2, 2000), postponed_since = COALESCE(postponed_since, now()),
			next_attempt_at = now() + make_interval(secs => CASE WHEN $3::bigint > 0 THEN LEAST($3::bigint, 86400)
				ELSE `+backoffSQL("attempts")+` END)
		WHERE track_id = $1 AND status = 'pending'
			AND (postponed_since IS NULL OR postponed_since > now() - make_interval(secs => $4::bigint))`,
		trackID, msg, int64(retryAfter.Seconds()), int64(PostponeMax.Seconds()))
	if err != nil {
		return false, fmt.Errorf("postpone enrichment %s: %w", trackID, err)
	}
	if tag.RowsAffected() == 0 {
		return false, q.featuresExist(ctx, trackID)
	}
	return true, nil
}

// featuresExist is nil when the track has a track_features row, else
// domain.ErrNotFound.
func (q *Queries) featuresExist(ctx context.Context, trackID string) error {
	var ok bool
	if err := q.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM track_features WHERE track_id = $1)`,
		trackID).Scan(&ok); err != nil {
		return fmt.Errorf("check track features %s: %w", trackID, err)
	}
	if !ok {
		return domain.ErrNotFound
	}
	return nil
}

// Backoff returns the retry delay after n failed attempts.
func Backoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := time.Duration(math.Pow(2, float64(n-1))) * time.Minute
	if d > 24*time.Hour || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// ImportFeatures stores a legacy enrichment result unless the track is
// already enriched in this database (newer data wins). It returns true when
// the row was written.
func (q *Queries) ImportFeatures(ctx context.Context, trackID, isrc string, found bool, f domain.AudioFeatures, genres []domain.Genre, source string) (bool, error) {
	status := domain.EnrichmentEnriched
	if !found {
		status = domain.EnrichmentUnavailable
		f, genres = domain.AudioFeatures{}, nil
	}
	var genresJSON []byte
	if genres != nil {
		b, err := json.Marshal(genres)
		if err != nil {
			return false, fmt.Errorf("marshal genres: %w", err)
		}
		genresJSON = b
	}
	tag, err := q.db.Exec(ctx, `
		INSERT INTO track_features (
			track_id, isrc, status, source, acousticness, danceability, energy, instrumentalness,
			key, liveness, loudness, mode, speechiness, tempo, time_signature, valence,
			genres, attempts, last_error, enriched_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 0, NULL, now())
		ON CONFLICT (track_id) DO UPDATE SET
			isrc = COALESCE(track_features.isrc, EXCLUDED.isrc),
			status = EXCLUDED.status, source = EXCLUDED.source,
			acousticness = EXCLUDED.acousticness, danceability = EXCLUDED.danceability,
			energy = EXCLUDED.energy, instrumentalness = EXCLUDED.instrumentalness,
			key = EXCLUDED.key, liveness = EXCLUDED.liveness, loudness = EXCLUDED.loudness,
			mode = EXCLUDED.mode, speechiness = EXCLUDED.speechiness, tempo = EXCLUDED.tempo,
			time_signature = EXCLUDED.time_signature, valence = EXCLUDED.valence,
			genres = EXCLUDED.genres, attempts = 0, last_error = NULL, enriched_at = now()
		WHERE track_features.status <> 'enriched'
			AND NOT (track_features.status = 'unavailable' AND EXCLUDED.status = 'unavailable')`,
		trackID, nullIfEmpty(isrc), status, nullIfEmpty(source),
		f.Acousticness, f.Danceability, f.Energy, f.Instrumentalness,
		f.Key, f.Liveness, f.Loudness, f.Mode, f.Speechiness, f.Tempo, f.TimeSignature, f.Valence,
		genresJSON)
	if err != nil {
		return false, fmt.Errorf("import features %s: %w", trackID, err)
	}
	return tag.RowsAffected() > 0, nil
}
