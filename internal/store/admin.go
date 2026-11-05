package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// TrackRow is a track in the admin tracks table.
type TrackRow struct {
	Track            domain.TrackView `json:"track"`
	Plays            int64            `json:"plays"`
	LastPlayed       *time.Time       `json:"last_played"`
	EnrichmentStatus string           `json:"enrichment_status"`
	CreatedAt        time.Time        `json:"created_at"`
}

// ArtistRow is an artist in the admin artists table.
type ArtistRow struct {
	Artist     domain.Artist `json:"artist"`
	Plays      int64         `json:"plays"`
	Tracks     int64         `json:"tracks"`
	LastPlayed *time.Time    `json:"last_played"`
}

// Enrichment is the full enrichment record of a track (admin view).
type Enrichment struct {
	Status        string               `json:"status"`
	Source        string               `json:"source"`
	Attempts      int32                `json:"attempts"`
	LastError     string               `json:"last_error"`
	NextAttemptAt *time.Time           `json:"next_attempt_at"`
	EnrichedAt    *time.Time           `json:"enriched_at"`
	Features      domain.AudioFeatures `json:"features"`
	Genres        []domain.Genre       `json:"genres"`
}

// TrackAdminDetail is a track with counters and enrichment details.
type TrackAdminDetail struct {
	domain.TrackView
	Plays      int64       `json:"plays"`
	LastPlayed *time.Time  `json:"last_played"`
	CreatedAt  time.Time   `json:"created_at"`
	Enrichment *Enrichment `json:"enrichment"`
}

// EnrichmentCounts counts tracks per enrichment status.
type EnrichmentCounts struct {
	Pending     int64 `json:"pending"`
	Enriched    int64 `json:"enriched"`
	Unavailable int64 `json:"unavailable"`
	Failed      int64 `json:"failed"`
}

// AuditEntry is one admin audit log row.
type AuditEntry struct {
	ID     int64           `json:"id"`
	At     time.Time       `json:"at"`
	Actor  string          `json:"actor"`
	Action string          `json:"action"`
	Detail json.RawMessage `json:"detail"`
	IP     string          `json:"ip"`
}

// likePattern escapes a user search term for ILIKE.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.TrimSpace(q)) + "%"
}

// AdminHistory lists history newest first, optionally filtered by a search
// over track, artist and album names.
func (q *Queries) AdminHistory(ctx context.Context, limit int, before time.Time, search string) ([]domain.HistoryEntry, error) {
	if before.IsZero() {
		before = time.Now().Add(time.Minute)
	}
	args := []any{before, limit}
	filter := ""
	if strings.TrimSpace(search) != "" {
		args = append(args, likePattern(search))
		filter = `AND (t.name ILIKE $3 OR al.name ILIKE $3 OR EXISTS (
			SELECT 1 FROM track_artists ta JOIN artists a ON a.id = ta.artist_id
			WHERE ta.track_id = t.id AND a.name ILIKE $3))`
	}
	rows, err := q.db.Query(ctx, `
		SELECT h.id::text, h.played_at, h.updated_at, h.replay_count, `+trackColumns+`
		FROM listening_history h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE h.played_at < $1 `+filter+`
		ORDER BY h.played_at DESC
		LIMIT $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("admin history: %w", err)
	}
	defer rows.Close()
	out := []domain.HistoryEntry{}
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
	return out, q.hydrate(ctx, views...)
}

// AdminTracks lists tracks with play counters and enrichment status.
func (q *Queries) AdminTracks(ctx context.Context, search, status, sort, order string, limit, offset int) ([]TrackRow, int64, error) {
	sortCol := map[string]string{
		"plays": "plays", "last_played": "last_played", "name": "t.name", "created": "t.created_at",
	}[sort]
	if sortCol == "" {
		sortCol = "last_played"
	}
	dir := "DESC"
	if strings.EqualFold(order, "asc") {
		dir = "ASC"
	}
	where := []string{"true"}
	args := []any{}
	if strings.TrimSpace(search) != "" {
		args = append(args, likePattern(search))
		n := len(args)
		where = append(where, fmt.Sprintf(`(t.name ILIKE $%d OR t.isrc ILIKE $%d OR al.name ILIKE $%d OR EXISTS (
			SELECT 1 FROM track_artists ta JOIN artists a ON a.id = ta.artist_id
			WHERE ta.track_id = t.id AND a.name ILIKE $%d))`, n, n, n, n))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("COALESCE(tf.status, 'pending') = $%d", len(args)))
	}
	base := `
		FROM tracks t
		LEFT JOIN albums al ON al.id = t.album_id
		LEFT JOIN track_features tf ON tf.track_id = t.id
		LEFT JOIN (
			SELECT track_id, SUM(replay_count)::bigint AS plays, MAX(played_at) AS last_played
			FROM listening_history GROUP BY track_id
		) p ON p.track_id = t.id
		WHERE ` + strings.Join(where, " AND ")

	var total int64
	if err := q.db.QueryRow(ctx, `SELECT COUNT(*) `+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("admin tracks count: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := q.db.Query(ctx, `
		SELECT COALESCE(p.plays, 0) AS plays, p.last_played AS last_played,
			COALESCE(tf.status, 'pending'), t.created_at, `+trackColumns+base+
		fmt.Sprintf(` ORDER BY %s %s NULLS LAST, t.id LIMIT $%d OFFSET $%d`, sortCol, dir, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("admin tracks: %w", err)
	}
	defer rows.Close()
	out := []TrackRow{}
	for rows.Next() {
		var tr TrackRow
		var al albumScan
		if err := rows.Scan(append(append([]any{&tr.Plays, &tr.LastPlayed, &tr.EnrichmentStatus, &tr.CreatedAt},
			scanTrack(&tr.Track)...), al.targets()...)...); err != nil {
			return nil, 0, err
		}
		tr.Track.Album = al.album()
		out = append(out, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	views := make([]*domain.TrackView, len(out))
	for i := range out {
		views[i] = &out[i].Track
	}
	return out, total, q.hydrate(ctx, views...)
}

// AdminArtists lists artists with play and track counters.
func (q *Queries) AdminArtists(ctx context.Context, search, sort, order string, limit, offset int) ([]ArtistRow, int64, error) {
	sortCol := map[string]string{"plays": "plays", "name": "a.name", "tracks": "tracks"}[sort]
	if sortCol == "" {
		sortCol = "plays"
	}
	dir := "DESC"
	if strings.EqualFold(order, "asc") {
		dir = "ASC"
	}
	args := []any{}
	where := "true"
	if strings.TrimSpace(search) != "" {
		args = append(args, likePattern(search))
		where = "a.name ILIKE $1"
	}
	base := `
		FROM artists a
		LEFT JOIN (
			SELECT ta.artist_id, COUNT(DISTINCT ta.track_id)::bigint AS tracks,
				COALESCE(SUM(p.plays), 0)::bigint AS plays, MAX(p.last_played) AS last_played
			FROM track_artists ta
			LEFT JOIN (
				SELECT track_id, SUM(replay_count) AS plays, MAX(played_at) AS last_played
				FROM listening_history GROUP BY track_id
			) p ON p.track_id = ta.track_id
			GROUP BY ta.artist_id
		) s ON s.artist_id = a.id
		WHERE ` + where
	var total int64
	if err := q.db.QueryRow(ctx, `SELECT COUNT(*) `+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("admin artists count: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := q.db.Query(ctx, `
		SELECT a.id, a.name, COALESCE(a.uri, ''), COALESCE(a.external_url, ''), COALESCE(a.image_url, ''),
			COALESCE(s.plays, 0) AS plays, COALESCE(s.tracks, 0) AS tracks, s.last_played`+base+
		fmt.Sprintf(` ORDER BY %s %s NULLS LAST, a.id LIMIT $%d OFFSET $%d`, sortCol, dir, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("admin artists: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ArtistRow, error) {
		var a ArtistRow
		err := r.Scan(&a.Artist.ID, &a.Artist.Name, &a.Artist.URI, &a.Artist.ExternalURL, &a.Artist.ImageURL, &a.Plays, &a.Tracks, &a.LastPlayed)
		return a, err
	})
	if out == nil {
		out = []ArtistRow{}
	}
	return out, total, err
}

// AdminTrack returns a track with full enrichment details.
func (q *Queries) AdminTrack(ctx context.Context, id string) (TrackAdminDetail, error) {
	var d TrackAdminDetail
	var al albumScan
	err := q.db.QueryRow(ctx, `SELECT `+trackColumns+`, t.created_at,
			(SELECT COALESCE(SUM(replay_count), 0)::bigint FROM listening_history WHERE track_id = t.id),
			(SELECT MAX(played_at) FROM listening_history WHERE track_id = t.id)
		FROM tracks t LEFT JOIN albums al ON al.id = t.album_id
		WHERE t.id = $1`, id).
		Scan(append(append(scanTrack(&d.TrackView), al.targets()...), &d.CreatedAt, &d.Plays, &d.LastPlayed)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.ErrNotFound
	}
	if err != nil {
		return d, fmt.Errorf("admin track: %w", err)
	}
	d.Album = al.album()
	if err := q.hydrate(ctx, &d.TrackView); err != nil {
		return d, err
	}

	var e Enrichment
	var source, lastErr *string
	var genres []byte
	f := &e.Features
	err = q.db.QueryRow(ctx, `
		SELECT status, source, attempts, last_error, next_attempt_at, enriched_at,
			acousticness, danceability, energy, instrumentalness, key, liveness, loudness, mode,
			speechiness, tempo, time_signature, valence, genres
		FROM track_features WHERE track_id = $1`, id).
		Scan(&e.Status, &source, &e.Attempts, &lastErr, &e.NextAttemptAt, &e.EnrichedAt,
			&f.Acousticness, &f.Danceability, &f.Energy, &f.Instrumentalness, &f.Key, &f.Liveness,
			&f.Loudness, &f.Mode, &f.Speechiness, &f.Tempo, &f.TimeSignature, &f.Valence, &genres)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("admin track features: %w", err)
	}
	e.Source, e.LastError = deref(source), deref(lastErr)
	e.Genres = []domain.Genre{}
	if len(genres) > 0 {
		_ = json.Unmarshal(genres, &e.Genres)
	}
	d.Enrichment = &e
	return d, nil
}

// RequeueTrack resets a track's enrichment to pending, due now. It returns
// the track's ISRC.
func (q *Queries) RequeueTrack(ctx context.Context, id string) (string, error) {
	var isrc *string
	err := q.db.QueryRow(ctx, `
		INSERT INTO track_features (track_id, isrc)
		SELECT id, isrc FROM tracks WHERE id = $1
		ON CONFLICT (track_id) DO UPDATE SET
			status = 'pending', attempts = 0, last_error = NULL, next_attempt_at = now(),
			isrc = COALESCE(track_features.isrc, EXCLUDED.isrc)
		RETURNING isrc`, id).Scan(&isrc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("requeue track: %w", err)
	}
	return deref(isrc), nil
}

// RequeueFailed resets every failed enrichment to pending.
func (q *Queries) RequeueFailed(ctx context.Context) (int64, error) {
	tag, err := q.db.Exec(ctx, `
		UPDATE track_features SET status = 'pending', attempts = 0, last_error = NULL, next_attempt_at = now()
		WHERE status = 'failed'`)
	if err != nil {
		return 0, fmt.Errorf("requeue failed: %w", err)
	}
	return tag.RowsAffected(), nil
}

// EnrichmentCounts counts tracks per enrichment status.
func (q *Queries) EnrichmentCounts(ctx context.Context) (EnrichmentCounts, error) {
	var c EnrichmentCounts
	err := q.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status = 'pending'), COUNT(*) FILTER (WHERE status = 'enriched'),
			COUNT(*) FILTER (WHERE status = 'unavailable'), COUNT(*) FILTER (WHERE status = 'failed')
		FROM track_features`).Scan(&c.Pending, &c.Enriched, &c.Unavailable, &c.Failed)
	if err != nil {
		return c, fmt.Errorf("enrichment counts: %w", err)
	}
	return c, nil
}

// Audit appends an audit log entry.
func (q *Queries) Audit(ctx context.Context, actor, action string, detail any, ip string) error {
	var raw []byte
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		raw = b
	}
	_, err := q.db.Exec(ctx, `INSERT INTO admin_audit (actor, action, detail, ip) VALUES ($1, $2, $3, $4)`,
		actor, action, raw, nullIfEmpty(ip))
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

// AuditLog lists audit entries with id < before (0 = newest), newest first.
func (q *Queries) AuditLog(ctx context.Context, limit int, before int64) ([]AuditEntry, error) {
	if before <= 0 {
		before = 1<<62 - 1
	}
	rows, err := q.db.Query(ctx, `
		SELECT id, at, actor, action, COALESCE(detail, 'null'::jsonb), COALESCE(ip, '')
		FROM admin_audit WHERE id < $1 ORDER BY id DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("audit log: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AuditEntry, error) {
		var e AuditEntry
		err := r.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.Detail, &e.IP)
		return e, err
	})
	if out == nil {
		out = []AuditEntry{}
	}
	return out, err
}
