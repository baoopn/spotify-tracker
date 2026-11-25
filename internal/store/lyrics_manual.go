package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// Manual lyrics (source 'manual', 00019) are lyrics the admin adds by hand.
// They win over LRCLIB: automatic lookups never claim or overwrite them
// (lyricsHeldSQL); only an explicit re-queue with replace_manual lets the
// next LRCLIB result replace them.

// ErrManualLyrics is returned by RequeueLyrics when the track has manual
// lyrics and the re-queue did not ask to replace them.
var ErrManualLyrics = errors.New("the track has manual lyrics")

// SetManualLyrics stores lyrics added by hand (status found or
// instrumental) as source 'manual', with by (the admin's email, or "mcp")
// and the time as the audit. Any pending lookup request is cleared: the
// re-queue mark, a re-queue taken by a running claim, a replace_manual
// request and the retry state. A live claim keeps its lease (it is not
// broken), but the lookup's result is not stored when it finishes
// (SetLyrics gives the lease back on a held row).
//
// It shares the hash rule of SetLyrics: when the lyrics changed (found, and
// a different source hash than before and than the translation's), an
// existing translation goes to the front of the translation queue
// (retranslate). No translation row is created (no backfill). Saving the
// same manual lyrics again touches nothing (stored is false). An unknown
// track is domain.ErrNotFound.
func (q *Queries) SetManualLyrics(ctx context.Context, trackID string, l domain.Lyrics, by string) (stored, retranslate bool, err error) {
	if l.Status != domain.LyricsFound && l.Status != domain.LyricsInstrumental {
		return false, false, fmt.Errorf("set manual lyrics %s: invalid status %q", trackID, l.Status)
	}
	if l.Status == domain.LyricsInstrumental {
		l.Plain, l.Synced = "", ""
	}
	var (
		known       bool
		n, requeued int64
	)
	err = q.db.QueryRow(ctx, `
		WITH old AS (
			SELECT l.track_id, `+srcHashSQL+` AS hash FROM lyrics l WHERE l.track_id = $1 FOR UPDATE),
		up AS (
			INSERT INTO lyrics AS l (track_id, status, source, plain_lyrics, synced_lyrics, manual_at, manual_by)
			SELECT t.id, $2, 'manual', $3, $4, now(), $5 FROM tracks t WHERE t.id = $1
			ON CONFLICT (track_id) DO UPDATE SET status = EXCLUDED.status, source = 'manual', lrclib_id = NULL,
				plain_lyrics = EXCLUDED.plain_lyrics, synced_lyrics = EXCLUDED.synced_lyrics,
				attempts = 0, last_error = NULL, next_attempt_at = NULL,
				requested_at = NULL, claim_requested_at = NULL, replace_manual = false,
				manual_at = now(), manual_by = EXCLUDED.manual_by
			WHERE l.source <> 'manual' OR l.replace_manual OR l.requested_at IS NOT NULL
				OR l.claim_requested_at IS NOT NULL OR l.next_attempt_at IS NOT NULL
				OR l.status IS DISTINCT FROM EXCLUDED.status
				OR l.plain_lyrics IS DISTINCT FROM EXCLUDED.plain_lyrics
				OR l.synced_lyrics IS DISTINCT FROM EXCLUDED.synced_lyrics
			RETURNING l.track_id, l.status, `+srcHashSQL+` AS hash),
		tr AS (
			UPDATE lyrics_translations tr SET requested_at = now(), updated_at = now()
			FROM up LEFT JOIN old ON old.track_id = up.track_id
			WHERE tr.track_id = up.track_id AND up.status = 'found' AND up.hash IS DISTINCT FROM old.hash
				AND up.hash <> tr.source_hash
			RETURNING 1)
		SELECT EXISTS (SELECT 1 FROM tracks WHERE id = $1), (SELECT count(*) FROM up), (SELECT count(*) FROM tr)`,
		trackID, l.Status, nullIfEmpty(l.Plain), nullIfEmpty(l.Synced), by).Scan(&known, &n, &requeued)
	if err != nil {
		return false, false, fmt.Errorf("set manual lyrics %s: %w", trackID, err)
	}
	if !known {
		return false, false, domain.ErrNotFound
	}
	return n > 0, requeued > 0, nil
}

// ClearManualLyrics removes a track's manual lyrics and re-queues an LRCLIB
// lookup at the front (status pending, source 'lrclib'). A track that cannot
// be looked up (no title or artist) loses its lyrics row instead. A live
// claim keeps its lease, and its result is now stored. It reports false when
// the track has no manual lyrics (unknown tracks included).
func (q *Queries) ClearManualLyrics(ctx context.Context, trackID string) (bool, error) {
	var n int64
	err := q.db.QueryRow(ctx, `
		WITH up AS (
			UPDATE lyrics l SET status = 'pending', source = 'lrclib', lrclib_id = NULL, plain_lyrics = NULL,
				synced_lyrics = NULL, attempts = 0, last_error = NULL, next_attempt_at = NULL,
				requested_at = now(), replace_manual = false, manual_at = NULL, manual_by = NULL
			FROM tracks t
			WHERE l.track_id = $1 AND t.id = l.track_id AND l.source = 'manual' AND `+lookupableSQL+`
			RETURNING 1),
		del AS (
			DELETE FROM lyrics l USING tracks t
			WHERE l.track_id = $1 AND t.id = l.track_id AND l.source = 'manual' AND NOT (`+lookupableSQL+`)
			RETURNING 1)
		SELECT (SELECT count(*) FROM up) + (SELECT count(*) FROM del)`, trackID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("clear manual lyrics %s: %w", trackID, err)
	}
	return n > 0, nil
}

// TrackDurationMS returns a track's length in ms (0 when unknown); an
// unknown track is domain.ErrNotFound.
func (q *Queries) TrackDurationMS(ctx context.Context, trackID string) (int64, error) {
	var d *int64
	err := q.db.QueryRow(ctx, `SELECT duration_ms::bigint FROM tracks WHERE id = $1`, trackID).Scan(&d)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("track duration %s: %w", trackID, err)
	}
	if d == nil {
		return 0, nil
	}
	return *d, nil
}
