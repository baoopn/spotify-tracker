package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// The LRCLIB lookup queue is the lyrics table itself (00017). A track with no
// lyrics row needs a lookup: ClaimLyrics first inserts 'pending' rows for the
// tracks next in line, then leases rows, so replicas never look up the same
// track. Unlike translations, the whole library is backfilled (LRCLIB is
// free), at the lowest priority.

// lyricsSoon is how far ahead a failed or not-found lookup's retry may be and
// still count as on its way.
const lyricsSoon = `interval '2 minutes'`

// lyricsLeasedSQL is true while a worker holds the lyrics row l.
const lyricsLeasedSQL = `COALESCE(l.claimed_until >= now(), false)`

// lyricsRequestedSQL is true when the lyrics row l was re-queued: waiting
// (requested_at), or taken by a claim (claim_requested_at; with an expired
// lease the worker crashed and the re-queue still counts).
const lyricsRequestedSQL = `(l.requested_at IS NOT NULL OR l.claim_requested_at IS NOT NULL)`

// lyricsHeldSQL is true when the lyrics row l holds manual lyrics that no
// lookup may replace: source 'manual' without an explicit re-queue with
// replace_manual (00019). Automatic lookups (backfill, retries, now-playing
// and prefetch wakes) never claim such a row, and a lookup that claimed it
// before the manual lyrics were set only gives its lease back.
const lyricsHeldSQL = `(l.source = 'manual' AND NOT l.replace_manual)`

// lyricsClaimableSQL is true when the lyrics row l can be claimed: no live
// lease, not held manual lyrics, and it waits (pending), was re-queued, or
// its retry is due (not_found, error, or a failed re-lookup of stored
// lyrics).
const lyricsClaimableSQL = `(l.claimed_until IS NULL OR l.claimed_until < now()) AND NOT ` + lyricsHeldSQL + `
	AND (l.status = 'pending' OR ` + lyricsRequestedSQL + ` OR l.next_attempt_at <= now())`

// lyricsWaitingSQL is true when the lyrics row l will be looked up soon:
// leased now, pending, re-queued, or a retry due within lyricsSoon. Held
// manual lyrics never wait (a lease left on one is only given back).
const lyricsWaitingSQL = `(NOT ` + lyricsHeldSQL + ` AND (` + lyricsLeasedSQL + ` OR l.status = 'pending'
	OR ` + lyricsRequestedSQL + ` OR COALESCE(l.next_attempt_at <= now() + ` + lyricsSoon + `, false)))`

// lyricsQueuedSQL is true when the lyrics row l waits for a worker now (no
// lease, not held manual lyrics): pending, re-queued, or a retry that is due.
const lyricsQueuedSQL = `(NOT ` + lyricsLeasedSQL + ` AND NOT ` + lyricsHeldSQL + ` AND (l.status = 'pending'
	OR ` + lyricsRequestedSQL + ` OR COALESCE(l.next_attempt_at <= now(), false)))`

// lookupableSQL is true when track t can be looked up: it has a title and a
// credited artist. Other tracks never get a lookup (nor a pending status).
const lookupableSQL = `t.name <> '' AND EXISTS (SELECT 1 FROM track_artists ta JOIN artists ar ON ar.id = ta.artist_id
	WHERE ta.track_id = t.id)`

// lastPlayedSQL joins p.last_played, the last play of track t.
const lastPlayedSQL = `LEFT JOIN LATERAL (SELECT MAX(h.played_at) AS last_played FROM listening_history h
	WHERE h.track_id = t.id) p ON true`

// ClaimLyrics leases up to limit tracks to look up to owner for lease and
// returns them in queue order: the priority tracks in their order (the song
// playing now, then the upcoming songs), then re-queued tracks (newest
// request first), then by last play (never played last), new tracks before
// retries. Tracks with no lyrics row get a 'pending' row first (at most limit
// per call, chosen in the same order and inserted in id order, so replicas
// inserting the same tracks cannot deadlock). Rows locked or leased by
// another worker are skipped.
func (q *Queries) ClaimLyrics(ctx context.Context, owner string, priority []string, lease time.Duration, limit int) ([]domain.LyricsCandidate, error) {
	if priority == nil {
		priority = []string{}
	}
	_, err := q.db.Exec(ctx, `
		INSERT INTO lyrics (track_id, status, source)
		SELECT c.id, 'pending', 'lrclib' FROM (
			SELECT t.id FROM tracks t
			`+lastPlayedSQL+`
			WHERE `+lookupableSQL+` AND NOT EXISTS (SELECT 1 FROM lyrics l WHERE l.track_id = t.id)
			ORDER BY COALESCE(array_position($1::text[], t.id), 2147483647), p.last_played DESC NULLS LAST, t.id
			LIMIT $2) c
		ORDER BY c.id
		ON CONFLICT (track_id) DO NOTHING`, priority, limit)
	if err != nil {
		return nil, fmt.Errorf("queue lyrics lookups: %w", err)
	}
	rows, err := q.db.Query(ctx, `
		WITH pick AS (
			SELECT l.track_id, t.name, a.names AS artists, COALESCE(al.name, '') AS album, t.duration_ms,
				CASE WHEN l.status = 'pending' THEN '' ELSE l.status END AS status, l.attempts,
				COALESCE(array_position($2::text[], l.track_id), 2147483647) AS prio,
				COALESCE(l.requested_at, l.claim_requested_at) AS requested_at, p.last_played
			FROM lyrics l
			JOIN tracks t ON t.id = l.track_id
			JOIN LATERAL (SELECT array_agg(ar.name ORDER BY ta.position, ar.name) AS names
				FROM track_artists ta JOIN artists ar ON ar.id = ta.artist_id
				WHERE ta.track_id = t.id HAVING count(*) > 0) a ON true
			LEFT JOIN albums al ON al.id = t.album_id
			`+lastPlayedSQL+`
			WHERE t.name <> '' AND `+lyricsClaimableSQL+`
			ORDER BY COALESCE(array_position($2::text[], l.track_id), 2147483647),
				COALESCE(l.requested_at, l.claim_requested_at) DESC NULLS LAST,
				p.last_played DESC NULLS LAST, (l.status = 'pending') DESC, l.track_id
			LIMIT $4
			FOR UPDATE OF l SKIP LOCKED)
		UPDATE lyrics l SET claimed_by = $1, claimed_until = now() + $3::interval,
			claim_requested_at = COALESCE(l.requested_at, l.claim_requested_at), requested_at = NULL
		FROM pick WHERE l.track_id = pick.track_id
		RETURNING pick.track_id, pick.name, pick.artists, pick.album, pick.duration_ms, pick.status, pick.attempts,
			pick.prio, pick.requested_at, pick.last_played`, owner, priority, lease, limit)
	if err != nil {
		return nil, fmt.Errorf("claim lyrics: %w", err)
	}
	defer rows.Close()
	type claimed struct {
		c         domain.LyricsCandidate
		prio      int32
		requested *time.Time
		played    *time.Time
	}
	var got []claimed
	for rows.Next() {
		var x claimed
		if err := rows.Scan(&x.c.TrackID, &x.c.Name, &x.c.Artists, &x.c.Album, &x.c.DurationMS, &x.c.Status,
			&x.c.Attempts, &x.prio, &x.requested, &x.played); err != nil {
			return nil, err
		}
		if len(x.c.Artists) > 0 {
			x.c.Artist = x.c.Artists[0]
		}
		got = append(got, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// UPDATE ... RETURNING has no order: restore the queue order.
	sort.SliceStable(got, func(i, j int) bool {
		a, b := got[i], got[j]
		if a.prio != b.prio {
			return a.prio < b.prio
		}
		if c := cmpTimeDesc(a.requested, b.requested); c != 0 {
			return c < 0
		}
		if c := cmpTimeDesc(a.played, b.played); c != 0 {
			return c < 0
		}
		if (a.c.Status == "") != (b.c.Status == "") {
			return a.c.Status == ""
		}
		return a.c.TrackID < b.c.TrackID
	})
	out := make([]domain.LyricsCandidate, len(got))
	for i, x := range got {
		out[i] = x.c
	}
	return out, nil
}

// SetLyrics stores a successful lookup (status found or instrumental), clears
// the retry state and releases the lease. It writes only while owner holds
// the claim and reports whether it was stored. A track re-queued while it
// was claimed (requested_at set again) is looked up again.
//
// Manual lyrics win: when the row holds manual lyrics that no re-queue with
// replace_manual asked to replace (lyricsHeldSQL), only the lease is given
// back and stored is false. The row is locked first (FOR UPDATE), so manual
// lyrics saved while the lookup ran are seen even when they were committed
// after this statement started. After a replace_manual re-queue the result
// replaces the manual lyrics (source becomes 'lrclib' again).
//
// When the stored lyrics changed (found, and a different source hash than
// before and than the translation's), an existing translation goes to the
// front of the translation queue in the same statement (retranslate). It
// only sets requested_at, so a live translation lease is kept: that worker's
// result no longer matches the lyrics and is not stored. No translation row
// is created (no backfill), and unchanged lyrics touch nothing.
func (q *Queries) SetLyrics(ctx context.Context, owner, trackID string, l domain.Lyrics) (stored, retranslate bool, err error) {
	if l.Status != domain.LyricsFound && l.Status != domain.LyricsInstrumental {
		return false, false, fmt.Errorf("set lyrics %s: invalid status %q", trackID, l.Status)
	}
	var lrclibID *int64
	if l.LRCLibID != 0 {
		lrclibID = &l.LRCLibID
	}
	var n, requeued int64
	err = q.db.QueryRow(ctx, `
		WITH old AS (
			SELECT l.track_id, `+srcHashSQL+` AS hash, `+lyricsHeldSQL+` AS held
			FROM lyrics l WHERE l.track_id = $1 AND l.claimed_by = $2
			FOR UPDATE),
		kept AS (
			UPDATE lyrics l SET claimed_by = NULL, claimed_until = NULL, claim_requested_at = NULL
			FROM old WHERE l.track_id = old.track_id AND l.claimed_by = $2 AND old.held
			RETURNING 1),
		up AS (
			UPDATE lyrics l SET status = $3, source = 'lrclib', lrclib_id = $4, plain_lyrics = $5,
				synced_lyrics = $6, attempts = 0, last_error = NULL, fetched_at = now(), next_attempt_at = NULL,
				claimed_by = NULL, claimed_until = NULL, claim_requested_at = NULL,
				replace_manual = false, manual_at = NULL, manual_by = NULL
			FROM old WHERE l.track_id = old.track_id AND l.claimed_by = $2 AND NOT old.held
			RETURNING l.track_id, l.status, `+srcHashSQL+` AS hash),
		tr AS (
			UPDATE lyrics_translations tr SET requested_at = now(), updated_at = now()
			FROM up JOIN old ON old.track_id = up.track_id
			WHERE tr.track_id = up.track_id AND up.status = 'found' AND up.hash <> old.hash
				AND up.hash <> tr.source_hash
			RETURNING 1)
		SELECT (SELECT count(*) FROM up), (SELECT count(*) FROM tr)`,
		trackID, owner, l.Status, lrclibID, nullIfEmpty(l.Plain), nullIfEmpty(l.Synced)).Scan(&n, &requeued)
	if err != nil {
		return false, false, fmt.Errorf("set lyrics %s: %w", trackID, err)
	}
	return n > 0, requeued > 0, nil
}

// DeferLyrics records a lookup that found nothing (not_found) or failed
// (error), to be retried at next, and releases the lease. attempts counts
// lookups in a row with the same status (it restarts at 1 when the status
// changes). Stored lyrics (found or instrumental, re-queued for a fresh
// lookup) are never overwritten: a failed re-lookup keeps them and is
// retried at next (attempts counts failed re-lookups, for the backoff); a
// re-lookup that finds nothing keeps them and ends. last_error is always
// the last lookup's. It writes only while owner holds the claim.
func (q *Queries) DeferLyrics(ctx context.Context, owner, trackID, status string, next time.Time, lastErr string) error {
	if status != domain.LyricsNotFound && status != domain.LyricsError {
		return fmt.Errorf("defer lyrics %s: invalid status %q", trackID, status)
	}
	// Held manual lyrics (saved while the lookup ran) only get the lease back.
	// A replace_manual re-queue that finds nothing ends and keeps the manual
	// lyrics; one that fails keeps the request and is retried.
	_, err := q.db.Exec(ctx, `
		UPDATE lyrics l SET
			attempts = CASE WHEN `+lyricsHeldSQL+` THEN l.attempts
				WHEN l.status IN ('found', 'instrumental') AND $3::text = 'not_found' THEN 0
				WHEN l.status IN ('found', 'instrumental') THEN l.attempts + 1
				WHEN l.status = $3 THEN l.attempts + 1 ELSE 1 END,
			next_attempt_at = CASE WHEN `+lyricsHeldSQL+` THEN l.next_attempt_at
				WHEN l.status IN ('found', 'instrumental') AND $3::text = 'not_found' THEN NULL
				ELSE $4::timestamptz END,
			status = CASE WHEN l.status IN ('found', 'instrumental') THEN l.status ELSE $3 END,
			last_error = CASE WHEN `+lyricsHeldSQL+` THEN l.last_error ELSE $5 END,
			fetched_at = CASE WHEN `+lyricsHeldSQL+` THEN l.fetched_at ELSE now() END,
			replace_manual = l.replace_manual AND $3::text = 'error',
			claimed_by = NULL, claimed_until = NULL, claim_requested_at = NULL
		WHERE l.track_id = $1 AND l.claimed_by = $2`,
		trackID, owner, status, next, nullIfEmpty(lastErr))
	if err != nil {
		return fmt.Errorf("defer lyrics %s: %w", trackID, err)
	}
	return nil
}

// ReleaseLyrics gives back a lease without recording an outcome (rate limit,
// shutdown, a new song started): the track stays queued as it was, a
// re-queued track still first in line.
func (q *Queries) ReleaseLyrics(ctx context.Context, owner, trackID string) error {
	_, err := q.db.Exec(ctx, `
		UPDATE lyrics SET claimed_by = NULL, claimed_until = NULL,
			requested_at = COALESCE(requested_at, claim_requested_at), claim_requested_at = NULL
		WHERE track_id = $1 AND claimed_by = $2`, trackID, owner)
	if err != nil {
		return fmt.Errorf("release lyrics %s: %w", trackID, err)
	}
	return nil
}

// RequeueLyrics puts a track at the front of the lookup queue, whatever its
// status (not looked up yet, not found, failed, or found: a fresh lookup
// replaces lyrics that changed on LRCLIB). Stored lyrics stay served until
// then. The error backoff restarts. A track a worker is looking up right now
// keeps its lease and is looked up again when that worker finishes. It
// reports false when the track is unknown or has no title or artist.
//
// Manual lyrics are kept: without replaceManual the re-queue changes nothing
// and fails with ErrManualLyrics. With replaceManual the next LRCLIB result
// replaces them (a lookup that finds nothing keeps them).
func (q *Queries) RequeueLyrics(ctx context.Context, trackID string, replaceManual bool) (bool, error) {
	tag, err := q.db.Exec(ctx, `
		INSERT INTO lyrics (track_id, status, source, requested_at)
		SELECT t.id, 'pending', 'lrclib', now() FROM tracks t
		WHERE t.id = $1 AND `+lookupableSQL+`
		ON CONFLICT (track_id) DO UPDATE SET requested_at = now(),
			attempts = CASE WHEN lyrics.claimed_until >= now() THEN lyrics.attempts ELSE 0 END,
			replace_manual = lyrics.source = 'manual'
		WHERE lyrics.source <> 'manual' OR $2::boolean`, trackID, replaceManual)
	if err != nil {
		return false, fmt.Errorf("requeue lyrics %s: %w", trackID, err)
	}
	if tag.RowsAffected() > 0 || replaceManual {
		return tag.RowsAffected() > 0, nil
	}
	var manual bool
	if err := q.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM lyrics WHERE track_id = $1 AND source = 'manual')`,
		trackID).Scan(&manual); err != nil {
		return false, fmt.Errorf("requeue lyrics %s: %w", trackID, err)
	}
	if manual {
		return false, ErrManualLyrics
	}
	return false, nil
}

// LyricsQueue summarizes the lyrics lookup queue (admin and MCP). The status
// counts are exclusive: a queued or leased track counts only there.
type LyricsQueue struct {
	Queued       int64      `json:"queued"`      // waiting for a worker, backlog included
	Requested    int64      `json:"requested"`   // of queued: re-queued by an admin
	Backlog      int64      `json:"backlog"`     // of queued: never looked up (no row yet, or pending)
	InProgress   int64      `json:"in_progress"` // leased by a worker now
	Found        int64      `json:"found"`
	Instrumental int64      `json:"instrumental"`
	NotFound     int64      `json:"not_found"` // retry scheduled
	Error        int64      `json:"error"`     // failed, retry scheduled
	NextRetryAt  *time.Time `json:"next_retry_at"`
	LastLookupAt *time.Time `json:"last_lookup_at"`
	Manual       int64      `json:"manual"` // of found and instrumental: added by hand (never looked up again on their own)
}

// LyricsQueueSummary counts the lyrics queue by state.
func (q *Queries) LyricsQueueSummary(ctx context.Context) (LyricsQueue, error) {
	var s LyricsQueue
	err := q.db.QueryRow(ctx, `
		WITH l AS (
			SELECT l.status, `+lyricsRequestedSQL+` AS requested, l.next_attempt_at, l.fetched_at,
				`+lyricsLeasedSQL+` AND NOT `+lyricsHeldSQL+` AS leased, `+lyricsQueuedSQL+` AS queued,
				l.source = 'manual' AS manual
			FROM lyrics l),
		b AS (SELECT count(*) AS n FROM tracks t
			WHERE `+lookupableSQL+` AND NOT EXISTS (SELECT 1 FROM lyrics l WHERE l.track_id = t.id))
		SELECT
			(SELECT count(*) FROM l WHERE queued) + b.n,
			(SELECT count(*) FROM l WHERE queued AND requested),
			b.n + (SELECT count(*) FROM l WHERE queued AND status = 'pending'),
			(SELECT count(*) FROM l WHERE leased),
			(SELECT count(*) FROM l WHERE NOT leased AND NOT queued AND status = 'found'),
			(SELECT count(*) FROM l WHERE NOT leased AND NOT queued AND status = 'instrumental'),
			(SELECT count(*) FROM l WHERE NOT leased AND NOT queued AND status = 'not_found'),
			(SELECT count(*) FROM l WHERE NOT leased AND NOT queued AND status = 'error'),
			(SELECT min(next_attempt_at) FROM l WHERE next_attempt_at > now()),
			(SELECT max(fetched_at) FROM l),
			(SELECT count(*) FROM l WHERE manual AND status IN ('found', 'instrumental'))
		FROM b`).Scan(&s.Queued, &s.Requested, &s.Backlog, &s.InProgress, &s.Found, &s.Instrumental,
		&s.NotFound, &s.Error, &s.NextRetryAt, &s.LastLookupAt, &s.Manual)
	if err != nil {
		return LyricsQueue{}, fmt.Errorf("lyrics queue summary: %w", err)
	}
	return s, nil
}
