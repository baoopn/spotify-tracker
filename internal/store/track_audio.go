package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// TrackAudio is one listen-along audio file in the bucket.
type TrackAudio struct {
	TrackID      string
	Ext          string
	SizeBytes    int64
	ContentType  *string
	ETag         *string
	LastModified *time.Time
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
}

// AudioTrack is the catalog track of an audio file (admin Audio files page).
type AudioTrack struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	DurationMS    int32              `json:"duration_ms"`
	Artists       []ListenPlayArtist `json:"artists"`
	AlbumName     string             `json:"album_name"`
	CoverImageURL string             `json:"cover_image_url"`
}

// AudioFileRow is one uploaded file with its track (null when the track is
// not in the catalog) and the owner's plays of it.
type AudioFileRow struct {
	TrackID      string      `json:"track_id"`
	Ext          string      `json:"ext"`
	SizeBytes    int64       `json:"size_bytes"`
	ContentType  *string     `json:"content_type"`
	LastModified *time.Time  `json:"last_modified"`
	FirstSeenAt  time.Time   `json:"first_seen_at"`
	Track        *AudioTrack `json:"track"`
	OwnerPlays   int64       `json:"owner_plays"`
}

// MissingAudioRow is a played track with no audio file.
type MissingAudioRow struct {
	Track        AudioTrack `json:"track"`
	OwnerPlays   int64      `json:"owner_plays"`
	LastPlayedAt time.Time  `json:"last_played_at"`
}

// AudioSummary counts the audio files.
type AudioSummary struct {
	Files           int64
	TotalBytes      int64
	TracksInCatalog int64
}

// TrackQuery filters the admin tracks list.
type TrackQuery struct {
	Search, Status, Lyrics, Sort, Order string
	// Audio is "" (no filter), "has" or "missing" (see ValidAudioFilter).
	Audio         string
	Limit, Offset int
}

// Audio filters of AdminTrackList.
const (
	AudioFilterHas     = "has"
	AudioFilterMissing = "missing"
)

// hasAudio is true when track t has an audio file.
const hasAudio = `EXISTS (SELECT 1 FROM track_audio au WHERE au.track_id = t.id)`

var audioFilters = map[string]string{
	AudioFilterHas:     hasAudio,
	AudioFilterMissing: "NOT " + hasAudio,
}

// ValidAudioFilter reports whether f is "" or a known audio filter.
func ValidAudioFilter(f string) bool {
	_, ok := audioFilters[f]
	return f == "" || ok
}

const trackAudioColumns = `track_id, ext, size_bytes, content_type, etag, last_modified, first_seen_at, last_seen_at`

func scanTrackAudio(row pgx.Row) (TrackAudio, error) {
	var a TrackAudio
	err := row.Scan(&a.TrackID, &a.Ext, &a.SizeBytes, &a.ContentType, &a.ETag, &a.LastModified, &a.FirstSeenAt, &a.LastSeenAt)
	return a, err
}

// TrackAudioByIDs returns the audio files of the tracks in ids that have
// one, in one query.
func (q *Queries) TrackAudioByIDs(ctx context.Context, ids []string) (map[string]TrackAudio, error) {
	out := make(map[string]TrackAudio, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.db.Query(ctx, `SELECT `+trackAudioColumns+` FROM track_audio WHERE track_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("track audio by ids: %w", err)
	}
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TrackAudio, error) { return scanTrackAudio(r) })
	if err != nil {
		return nil, fmt.Errorf("track audio by ids: %w", err)
	}
	for _, a := range got {
		out[a.TrackID] = a
	}
	return out, nil
}

// TrackAudioFor returns the audio file of a track, or domain.ErrNotFound.
func (q *Queries) TrackAudioFor(ctx context.Context, trackID string) (TrackAudio, error) {
	a, err := scanTrackAudio(q.db.QueryRow(ctx, `SELECT `+trackAudioColumns+` FROM track_audio WHERE track_id = $1`, trackID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackAudio{}, domain.ErrNotFound
	}
	if err != nil {
		return TrackAudio{}, fmt.Errorf("track audio: %w", err)
	}
	return a, nil
}

const upsertTrackAudio = `
	INSERT INTO track_audio (track_id, ext, size_bytes, content_type, etag, last_modified)
	SELECT * FROM unnest($1::text[], $2::text[], $3::bigint[], $4::text[], $5::text[], $6::timestamptz[])
	ON CONFLICT (track_id) DO UPDATE SET ext = EXCLUDED.ext, size_bytes = EXCLUDED.size_bytes,
		content_type = EXCLUDED.content_type, etag = EXCLUDED.etag, last_modified = EXCLUDED.last_modified,
		last_seen_at = now()
	RETURNING track_id, (xmax = 0)`

func upsertArgs(files []TrackAudio) []any {
	ids, exts, types, etags := make([]string, len(files)), make([]string, len(files)), make([]*string, len(files)), make([]*string, len(files))
	sizes, mods := make([]int64, len(files)), make([]*time.Time, len(files))
	for i, f := range files {
		ids[i], exts[i], sizes[i], types[i], etags[i], mods[i] = f.TrackID, f.Ext, f.SizeBytes, f.ContentType, f.ETag, f.LastModified
	}
	return []any{ids, exts, sizes, types, etags, mods}
}

// UpsertTrackAudio records one audio file (found by an audio lookup).
func (q *Queries) UpsertTrackAudio(ctx context.Context, a TrackAudio) error {
	rows, err := q.db.Query(ctx, upsertTrackAudio, upsertArgs([]TrackAudio{a})...)
	if err != nil {
		return fmt.Errorf("upsert track audio: %w", err)
	}
	rows.Close()
	return rows.Err()
}

// ReplaceTrackAudio makes the table match a complete bucket listing: it
// upserts files and deletes every other row, in one transaction. It returns
// the IDs added and removed. Call it only with a complete listing.
func (s *Store) ReplaceTrackAudio(ctx context.Context, files []TrackAudio) (added, removed []string, err error) {
	err = s.InTx(ctx, func(q *Queries) error {
		added, removed = nil, nil
		ids := make([]string, len(files))
		for i, f := range files {
			ids[i] = f.TrackID
		}
		if len(files) > 0 {
			rows, err := q.db.Query(ctx, upsertTrackAudio, upsertArgs(files)...)
			if err != nil {
				return fmt.Errorf("upsert track audio: %w", err)
			}
			// RETURNING order is not guaranteed: each row names its track.
			for rows.Next() {
				var id string
				var inserted bool
				if err := rows.Scan(&id, &inserted); err != nil {
					rows.Close()
					return err
				}
				if inserted {
					added = append(added, id)
				}
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("upsert track audio: %w", err)
			}
		}
		rows, err := q.db.Query(ctx, `DELETE FROM track_audio WHERE NOT (track_id = ANY($1)) RETURNING track_id`, ids)
		if err != nil {
			return fmt.Errorf("delete track audio: %w", err)
		}
		removed, err = pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("delete track audio: %w", err)
		}
		return nil
	})
	return added, removed, err
}

// AudioSummary counts the files, their size and how many have a catalog
// track.
func (q *Queries) AudioSummary(ctx context.Context) (AudioSummary, error) {
	var s AudioSummary
	err := q.db.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(a.size_bytes), 0)::bigint, COUNT(t.id)
		FROM track_audio a LEFT JOIN tracks t ON t.id = a.track_id`).Scan(&s.Files, &s.TotalBytes, &s.TracksInCatalog)
	if err != nil {
		return s, fmt.Errorf("audio summary: %w", err)
	}
	return s, nil
}

// audioTracks loads the catalog tracks of ids with their artists.
func (q *Queries) audioTracks(ctx context.Context, ids []string) (map[string]AudioTrack, error) {
	out := make(map[string]AudioTrack, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.db.Query(ctx, `SELECT t.id, t.name, t.duration_ms, COALESCE(al.name, ''), COALESCE(al.cover_image_url, '')
		FROM tracks t LEFT JOIN albums al ON al.id = t.album_id WHERE t.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("audio tracks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t AudioTrack
		if err := rows.Scan(&t.ID, &t.Name, &t.DurationMS, &t.AlbumName, &t.CoverImageURL); err != nil {
			return nil, err
		}
		out[t.ID] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	artists, err := q.artistsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, t := range out {
		t.Artists = make([]ListenPlayArtist, 0, len(artists[id]))
		for _, a := range artists[id] {
			t.Artists = append(t.Artists, ListenPlayArtist{ID: a.ID, Name: a.Name})
		}
		out[id] = t
	}
	return out, nil
}

// ownerPlays is the owner's play count per track.
const ownerPlays = `(SELECT COALESCE(SUM(h.replay_count), 0)::bigint FROM listening_history h WHERE h.track_id = a.track_id)`

// AudioFiles lists the audio files, newest upload first, optionally
// searched by track or artist name.
func (q *Queries) AudioFiles(ctx context.Context, search string, limit, offset int) ([]AudioFileRow, int64, error) {
	where, args := "true", []any{}
	if strings.TrimSpace(search) != "" {
		args = append(args, likePattern(search))
		where = `(` + foldMatch("t.name", 1) + ` OR EXISTS (
			SELECT 1 FROM track_artists ta JOIN artists ar ON ar.id = ta.artist_id
			WHERE ta.track_id = t.id AND ` + foldMatch("ar.name", 1) + `))`
	}
	base := ` FROM track_audio a LEFT JOIN tracks t ON t.id = a.track_id WHERE ` + where
	var total int64
	if err := q.db.QueryRow(ctx, `SELECT COUNT(*)`+base, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audio files count: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := q.db.Query(ctx, `SELECT a.track_id, a.ext, a.size_bytes, a.content_type, a.last_modified, a.first_seen_at, `+
		ownerPlays+base+fmt.Sprintf(` ORDER BY a.last_modified DESC NULLS LAST, a.track_id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("audio files: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AudioFileRow, error) {
		var f AudioFileRow
		err := r.Scan(&f.TrackID, &f.Ext, &f.SizeBytes, &f.ContentType, &f.LastModified, &f.FirstSeenAt, &f.OwnerPlays)
		return f, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("audio files: %w", err)
	}
	ids := make([]string, len(out))
	for i := range out {
		ids[i] = out[i].TrackID
	}
	tracks, err := q.audioTracks(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		if t, ok := tracks[out[i].TrackID]; ok {
			out[i].Track = &t
		}
		out[i].FirstSeenAt = out[i].FirstSeenAt.UTC()
		if out[i].LastModified != nil {
			lm := out[i].LastModified.UTC()
			out[i].LastModified = &lm
		}
	}
	if out == nil {
		out = []AudioFileRow{}
	}
	return out, total, nil
}

// MissingAudio lists the owner's most-played tracks (all time) that have no
// audio file, optionally searched like the admin tracks list (trackSearch).
// total counts the matching tracks.
func (q *Queries) MissingAudio(ctx context.Context, search string, limit, offset int) ([]MissingAudioRow, int64, error) {
	filter, args := trackSearch(search, 0)
	if filter != "" {
		filter = "AND " + filter
	}
	base := `FROM listening_history h
		JOIN tracks t ON t.id = h.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE NOT EXISTS (SELECT 1 FROM track_audio au WHERE au.track_id = h.track_id) ` + filter + `
		GROUP BY h.track_id`
	var total int64
	if err := q.db.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT h.track_id `+base+`) x`, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("missing audio count: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := q.db.Query(ctx, `SELECT h.track_id, SUM(h.replay_count)::bigint AS plays, MAX(h.played_at) AS last `+base+
		fmt.Sprintf(` ORDER BY plays DESC, last DESC, h.track_id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("missing audio: %w", err)
	}
	type row struct {
		id    string
		plays int64
		last  time.Time
	}
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.id, &x.plays, &x.last)
		return x, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("missing audio: %w", err)
	}
	ids := make([]string, len(got))
	for i, x := range got {
		ids[i] = x.id
	}
	tracks, err := q.audioTracks(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]MissingAudioRow, 0, len(got))
	for _, x := range got {
		t, ok := tracks[x.id]
		if !ok {
			t = AudioTrack{ID: x.id, Artists: []ListenPlayArtist{}}
		}
		out = append(out, MissingAudioRow{Track: t, OwnerPlays: x.plays, LastPlayedAt: x.last.UTC()})
	}
	return out, total, nil
}

// ListenStorage is the listen-along storage summary for the admin overview.
type ListenStorage struct {
	Files             int64
	TotalBytes        int64
	TracksPlayed      int64 // distinct tracks in the owner's history
	TracksWithAudio   int64 // of those, with an audio file
	Plays30d          int64 // the owner's plays in the last 30 days
	Plays30dWithAudio int64 // of those, of a track with an audio file
	Added7d           int64 // files first seen in the last 7 days
}

// ListenStorageStats computes ListenStorage in one query.
func (q *Queries) ListenStorageStats(ctx context.Context) (ListenStorage, error) {
	var s ListenStorage
	err := q.db.QueryRow(ctx, `
		WITH played AS (SELECT DISTINCT track_id FROM listening_history),
		recent AS (
			SELECT h.replay_count, EXISTS (SELECT 1 FROM track_audio a WHERE a.track_id = h.track_id) AS has_audio
			FROM listening_history h WHERE h.played_at >= now() - interval '30 days'
		)
		SELECT
			(SELECT COUNT(*) FROM track_audio),
			(SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM track_audio),
			(SELECT COUNT(*) FROM played),
			(SELECT COUNT(*) FROM played p JOIN track_audio a ON a.track_id = p.track_id),
			(SELECT COALESCE(SUM(replay_count), 0)::bigint FROM recent),
			(SELECT COALESCE(SUM(replay_count) FILTER (WHERE has_audio), 0)::bigint FROM recent),
			(SELECT COUNT(*) FROM track_audio WHERE first_seen_at >= now() - interval '7 days')`).
		Scan(&s.Files, &s.TotalBytes, &s.TracksPlayed, &s.TracksWithAudio, &s.Plays30d, &s.Plays30dWithAudio, &s.Added7d)
	if err != nil {
		return s, fmt.Errorf("listen storage stats: %w", err)
	}
	return s, nil
}
