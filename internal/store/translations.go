package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// srcHashSQL is the hash of the lyrics a translation was made from (alias l
// = lyrics). It is computed only in SQL so every query agrees.
const srcHashSQL = `md5(COALESCE(l.synced_lyrics, '') || E'\x1f' || COALESCE(l.plain_lyrics, ''))`

// The translation queue is lyrics_translations itself. A row exists only for
// songs that were queued: played while the feature is on (now playing, or a
// recent play) or re-queued by an admin. The library is never backfilled.
// A row is claimable when it waits (pending), is stale (the lyrics changed),
// or failed and its retry is due, and no live lease holds it.
const claimableSQL = `(tr.claimed_until IS NULL OR tr.claimed_until < now())
	AND (tr.status = 'pending' OR tr.source_hash <> ` + srcHashSQL + `
		OR (tr.status = 'error' AND tr.next_attempt_at <= now()))`

// EnqueueTranslation queues one track (a song playing now) when it has found
// lyrics and no translation row yet. It reports whether a row was added.
func (q *Queries) EnqueueTranslation(ctx context.Context, trackID string) (bool, error) {
	tag, err := q.db.Exec(ctx, `
		INSERT INTO lyrics_translations (track_id, status, source_hash)
		SELECT l.track_id, 'pending', `+srcHashSQL+` FROM lyrics l
		WHERE l.track_id = $1 AND l.status = 'found'
		ON CONFLICT (track_id) DO NOTHING`, trackID)
	if err != nil {
		return false, fmt.Errorf("enqueue translation %s: %w", trackID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// EnqueueRecentTranslations queues tracks played within window that have
// found lyrics and no translation row (covers lyrics found after a song
// started, and plays another replica saw). It returns how many were added.
func (q *Queries) EnqueueRecentTranslations(ctx context.Context, window time.Duration) (int64, error) {
	tag, err := q.db.Exec(ctx, `
		INSERT INTO lyrics_translations (track_id, status, source_hash)
		SELECT l.track_id, 'pending', `+srcHashSQL+` FROM lyrics l
		WHERE l.status = 'found' AND EXISTS (SELECT 1 FROM listening_history h
			WHERE h.track_id = l.track_id AND h.played_at >= now() - $1::interval)
		ON CONFLICT (track_id) DO NOTHING`, window)
	if err != nil {
		return 0, fmt.Errorf("enqueue recent translations: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ClaimTranslations leases up to limit queued songs to owner for lease and
// returns them in queue order: the priority track (the song playing now)
// first, then re-queued songs (newest request first), then by last play.
// Rows locked or leased by another worker are skipped.
func (q *Queries) ClaimTranslations(ctx context.Context, owner, priority string, lease time.Duration, limit int) ([]domain.TranslationCandidate, error) {
	rows, err := q.db.Query(ctx, `
		WITH pick AS (
			SELECT tr.track_id, t.name, COALESCE(a.name, '') AS artist, COALESCE(l.synced_lyrics, '') AS synced,
				COALESCE(l.plain_lyrics, '') AS plain, `+srcHashSQL+` AS hash, tr.status, tr.attempts,
				tr.source_hash <> `+srcHashSQL+` AS stale,
				(tr.track_id = $2) AS prio, tr.requested_at, p.last_played
			FROM lyrics_translations tr
			JOIN lyrics l ON l.track_id = tr.track_id AND l.status = 'found'
			JOIN tracks t ON t.id = tr.track_id
			LEFT JOIN LATERAL (SELECT ar.name FROM track_artists ta JOIN artists ar ON ar.id = ta.artist_id
				WHERE ta.track_id = t.id ORDER BY ta.position, ar.name LIMIT 1) a ON true
			LEFT JOIN LATERAL (SELECT MAX(h.played_at) AS last_played FROM listening_history h
				WHERE h.track_id = tr.track_id) p ON true
			WHERE `+claimableSQL+`
			ORDER BY (tr.track_id = $2) DESC, tr.requested_at DESC NULLS LAST, p.last_played DESC NULLS LAST, tr.track_id
			LIMIT $4
			FOR UPDATE OF tr SKIP LOCKED)
		UPDATE lyrics_translations tr SET claimed_by = $1, claimed_until = now() + $3::interval
		FROM pick WHERE tr.track_id = pick.track_id
		RETURNING pick.track_id, pick.name, pick.artist, pick.synced, pick.plain, pick.hash,
			CASE WHEN pick.stale THEN '' ELSE pick.status END, CASE WHEN pick.stale THEN 0 ELSE pick.attempts END,
			pick.prio, pick.requested_at, pick.last_played`, owner, priority, lease, limit)
	if err != nil {
		return nil, fmt.Errorf("claim translations: %w", err)
	}
	defer rows.Close()
	type claimed struct {
		c         domain.TranslationCandidate
		prio      bool
		requested *time.Time
		played    *time.Time
	}
	var got []claimed
	for rows.Next() {
		var x claimed
		if err := rows.Scan(&x.c.TrackID, &x.c.Name, &x.c.Artist, &x.c.Synced, &x.c.Plain, &x.c.SourceHash,
			&x.c.Status, &x.c.Attempts, &x.prio, &x.requested, &x.played); err != nil {
			return nil, err
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
			return a.prio
		}
		if c := cmpTimeDesc(a.requested, b.requested); c != 0 {
			return c < 0
		}
		if c := cmpTimeDesc(a.played, b.played); c != 0 {
			return c < 0
		}
		return a.c.TrackID < b.c.TrackID
	})
	out := make([]domain.TranslationCandidate, len(got))
	for i, x := range got {
		out[i] = x.c
	}
	return out, nil
}

// SetTranslation stores a finished translation and releases the lease. It
// writes only while owner holds the claim and the lyrics still match
// sourceHash; it reports whether it was stored.
func (q *Queries) SetTranslation(ctx context.Context, owner, trackID, sourceHash string, r domain.Translation) (bool, error) {
	if r.Kind != domain.TranslationSynced && r.Kind != domain.TranslationPlain {
		return false, fmt.Errorf("set translation %s: invalid kind %q", trackID, r.Kind)
	}
	tag, err := q.db.Exec(ctx, `
		UPDATE lyrics_translations tr SET status = 'done', source_hash = $3, kind = $4, source_language = $5,
			en_lines = $6, vi_lines = $7, en_text = $8, vi_text = $9, model = $10,
			input_tokens = $11, output_tokens = $12, attempts = 0, last_error = NULL,
			translated_at = now(), updated_at = now(), next_attempt_at = NULL, requested_at = NULL,
			claimed_by = NULL, claimed_until = NULL
		WHERE tr.track_id = $2 AND tr.claimed_by = $1
			AND EXISTS (SELECT 1 FROM lyrics l WHERE l.track_id = $2 AND l.status = 'found' AND `+srcHashSQL+` = $3)`,
		owner, trackID, sourceHash, r.Kind, nullIfEmpty(r.SourceLanguage), r.EN, r.VI,
		nullIfEmpty(r.ENText), nullIfEmpty(r.VIText), nullIfEmpty(r.Model),
		clampInt32(r.InputTokens), clampInt32(r.OutputTokens))
	if err != nil {
		return false, fmt.Errorf("set translation %s: %w", trackID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// DeferTranslation records a translation that was skipped, refused or failed
// (status error, retried at next; nil next = given up) and releases the
// lease. attempts counts outcomes in a row with the same status and lyrics.
// It writes only while owner holds the claim.
func (q *Queries) DeferTranslation(ctx context.Context, owner, trackID, sourceHash, status string, next *time.Time, lastErr string) error {
	switch status {
	case domain.TranslationSkipped, domain.TranslationRefused, domain.TranslationError:
	default:
		return fmt.Errorf("defer translation %s: invalid status %q", trackID, status)
	}
	_, err := q.db.Exec(ctx, `
		UPDATE lyrics_translations tr SET
			attempts = CASE WHEN tr.status = $4 AND tr.source_hash = $3 THEN tr.attempts + 1 ELSE 1 END,
			status = $4, source_hash = $3, last_error = $6, updated_at = now(), next_attempt_at = $5,
			requested_at = NULL, claimed_by = NULL, claimed_until = NULL
		WHERE tr.track_id = $2 AND tr.claimed_by = $1`,
		owner, trackID, sourceHash, status, next, nullIfEmpty(lastErr))
	if err != nil {
		return fmt.Errorf("defer translation %s: %w", trackID, err)
	}
	return nil
}

// ReleaseTranslation gives back a lease without recording an outcome (rate
// limit, cap reached, shutdown): the song stays queued as it was.
func (q *Queries) ReleaseTranslation(ctx context.Context, owner, trackID string) error {
	_, err := q.db.Exec(ctx, `
		UPDATE lyrics_translations SET claimed_by = NULL, claimed_until = NULL
		WHERE track_id = $2 AND claimed_by = $1`, owner, trackID)
	if err != nil {
		return fmt.Errorf("release translation %s: %w", trackID, err)
	}
	return nil
}

// RequeueTranslation puts a track with found lyrics at the front of the
// queue, clearing a refused, failed, given-up or done translation. It
// reports false when the track has no found lyrics.
func (q *Queries) RequeueTranslation(ctx context.Context, trackID string) (bool, error) {
	tag, err := q.db.Exec(ctx, `
		INSERT INTO lyrics_translations (track_id, status, source_hash, requested_at)
		SELECT l.track_id, 'pending', `+srcHashSQL+`, now() FROM lyrics l
		WHERE l.track_id = $1 AND l.status = 'found'
		ON CONFLICT (track_id) DO UPDATE SET status = 'pending', source_hash = EXCLUDED.source_hash,
			attempts = 0, last_error = NULL, next_attempt_at = NULL, requested_at = now(),
			claimed_by = NULL, claimed_until = NULL, updated_at = now()`, trackID)
	if err != nil {
		return false, fmt.Errorf("requeue translation %s: %w", trackID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// TranslationQueue summarizes the translation queue (admin and MCP).
type TranslationQueue struct {
	Queued      int64      `json:"queued"`      // waiting, or stale, or a retry that is due
	InProgress  int64      `json:"in_progress"` // leased by a worker now
	Done        int64      `json:"done"`
	Skipped     int64      `json:"skipped"`
	Refused     int64      `json:"refused"`
	Error       int64      `json:"error"`    // failed, retry scheduled
	GivenUp     int64      `json:"given_up"` // failed too often; re-queue to try again
	NextRetryAt *time.Time `json:"next_retry_at"`
	CallsToday  int64      `json:"calls_today"`
	CallsMonth  int64      `json:"calls_month"`
	TokensMonth struct {
		Input  int64 `json:"input"`
		Output int64 `json:"output"`
	} `json:"tokens_month"`
}

// TranslationQueueSummary counts the queue by state and reads today's and
// this month's (UTC) Claude calls.
func (q *Queries) TranslationQueueSummary(ctx context.Context, now time.Time) (TranslationQueue, error) {
	var s TranslationQueue
	day := now.UTC().Truncate(24 * time.Hour)
	err := q.db.QueryRow(ctx, `
		WITH tr AS (
			SELECT tr.*, tr.source_hash <> `+srcHashSQL+` AS stale,
				(tr.claimed_until IS NOT NULL AND tr.claimed_until >= now()) AS leased
			FROM lyrics_translations tr JOIN lyrics l ON l.track_id = tr.track_id AND l.status = 'found'),
		u AS (SELECT COALESCE(SUM(calls) FILTER (WHERE day = $1::date), 0) AS today,
				COALESCE(SUM(calls), 0) AS month, COALESCE(SUM(input_tokens), 0) AS tin,
				COALESCE(SUM(output_tokens), 0) AS tout
			FROM translation_usage
			WHERE day >= date_trunc('month', $1::date) AND day < date_trunc('month', $1::date) + interval '1 month')
		SELECT
			(SELECT count(*) FROM tr WHERE NOT leased AND (status = 'pending' OR stale
				OR (status = 'error' AND next_attempt_at <= now()))),
			(SELECT count(*) FROM tr WHERE leased),
			(SELECT count(*) FROM tr WHERE NOT leased AND NOT stale AND status = 'done'),
			(SELECT count(*) FROM tr WHERE NOT leased AND NOT stale AND status = 'skipped'),
			(SELECT count(*) FROM tr WHERE NOT leased AND NOT stale AND status = 'refused'),
			(SELECT count(*) FROM tr WHERE NOT leased AND NOT stale AND status = 'error' AND next_attempt_at > now()),
			(SELECT count(*) FROM tr WHERE NOT leased AND NOT stale AND status = 'error' AND next_attempt_at IS NULL),
			(SELECT min(next_attempt_at) FROM tr WHERE status = 'error' AND next_attempt_at > now()),
			u.today, u.month, u.tin, u.tout
		FROM u`, day).Scan(&s.Queued, &s.InProgress, &s.Done, &s.Skipped, &s.Refused, &s.Error, &s.GivenUp,
		&s.NextRetryAt, &s.CallsToday, &s.CallsMonth, &s.TokensMonth.Input, &s.TokensMonth.Output)
	if err != nil {
		return TranslationQueue{}, fmt.Errorf("translation queue summary: %w", err)
	}
	return s, nil
}

// ReserveTranslationCall counts one Claude call on day (UTC) if neither cap
// is reached (0 = no cap of that kind). It reports whether the call may go.
func (q *Queries) ReserveTranslationCall(ctx context.Context, day time.Time, dailyCap, monthlyCap int) (bool, error) {
	var calls int
	err := q.db.QueryRow(ctx, `
		WITH d AS (SELECT COALESCE((SELECT calls FROM translation_usage WHERE day = $1::date), 0) AS n),
			m AS (SELECT COALESCE(SUM(calls), 0) AS n FROM translation_usage
				WHERE day >= date_trunc('month', $1::date) AND day < date_trunc('month', $1::date) + interval '1 month')
		INSERT INTO translation_usage (day, calls)
		SELECT $1::date, 1 FROM d, m WHERE ($2 = 0 OR d.n < $2) AND ($3 = 0 OR m.n < $3)
		ON CONFLICT (day) DO UPDATE SET calls = translation_usage.calls + 1
		RETURNING calls`, day.UTC().Truncate(24*time.Hour), dailyCap, monthlyCap).Scan(&calls)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve translation call: %w", err)
	}
	return true, nil
}

// AddTranslationTokens adds the tokens of one call to day's usage.
func (q *Queries) AddTranslationTokens(ctx context.Context, day time.Time, in, out int64) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO translation_usage (day, input_tokens, output_tokens) VALUES ($1::date, $2, $3)
		ON CONFLICT (day) DO UPDATE SET input_tokens = translation_usage.input_tokens + EXCLUDED.input_tokens,
			output_tokens = translation_usage.output_tokens + EXCLUDED.output_tokens`,
		day.UTC().Truncate(24*time.Hour), in, out)
	if err != nil {
		return fmt.Errorf("add translation tokens: %w", err)
	}
	return nil
}

func clampInt32(n int64) int32 {
	return int32(max(0, min(n, 1<<31-1)))
}

// cmpTimeDesc orders newer times first and nil last: <0 when a goes first.
func cmpTimeDesc(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case a.After(*b):
		return -1
	case b.After(*a):
		return 1
	}
	return 0
}
