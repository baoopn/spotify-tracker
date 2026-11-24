package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// TrackLyrics is the lyrics record of one track (admin and MCP only).
type TrackLyrics struct {
	TrackID       string     `json:"track_id"`
	Status        string     `json:"status"`
	Source        string     `json:"source"`
	LRCLibID      *int64     `json:"lrclib_id"`
	Plain         *string    `json:"plain"`
	Synced        *string    `json:"synced,omitempty"`
	HasSynced     bool       `json:"has_synced"`
	Attempts      int32      `json:"attempts"`
	LastError     string     `json:"last_error"`
	FetchedAt     *time.Time `json:"fetched_at"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	// Translation is the Claude translation state; null unless status is found.
	Translation *TrackTranslation `json:"translation"`
}

// TrackTranslation is the translation of a track's lyrics (admin and MCP).
// Synced translations are joined into one line per parsed LRC line.
type TrackTranslation struct {
	// Status is done, skipped, refused, error, or pending (none yet).
	Status         string     `json:"status"`
	Stale          bool       `json:"stale"` // the lyrics changed since; it is being redone
	Kind           *string    `json:"kind"`
	SourceLanguage *string    `json:"source_language"`
	EN             *string    `json:"en"`
	VI             *string    `json:"vi"`
	Model          string     `json:"model"`
	Attempts       int32      `json:"attempts"`
	LastError      string     `json:"last_error"`
	TranslatedAt   *time.Time `json:"translated_at"`
	NextAttemptAt  *time.Time `json:"next_attempt_at"`
}

// LyricsMatch is one lyrics search result.
type LyricsMatch struct {
	Track       domain.TrackView `json:"track"`
	Snippet     string           `json:"snippet"`
	PhraseMatch bool             `json:"phrase_match"`
	HasSynced   bool             `json:"has_synced"`
}

// maxSnippet is the longest search snippet, in runes.
const maxSnippet = 240

// searchTimeout bounds one lyrics search (it can be called through MCP).
const searchTimeout = 5 * time.Second

// TracksNeedingLyrics returns up to limit tracks to look up: the priority
// track (the song playing now, "" = none) first, then tracks with no lyrics
// row, then not-found and failed lookups whose retry is due; most recently
// played first.
func (q *Queries) TracksNeedingLyrics(ctx context.Context, limit int, priority string) ([]domain.LyricsCandidate, error) {
	rows, err := q.db.Query(ctx, `
		SELECT t.id, t.name, a.name, COALESCE(al.name, ''), t.duration_ms, COALESCE(l.status, ''), COALESCE(l.attempts, 0)
		FROM tracks t
		JOIN LATERAL (SELECT ar.name FROM track_artists ta JOIN artists ar ON ar.id = ta.artist_id
			WHERE ta.track_id = t.id ORDER BY ta.position, ar.name LIMIT 1) a ON true
		LEFT JOIN albums al ON al.id = t.album_id
		LEFT JOIN lyrics l ON l.track_id = t.id
		LEFT JOIN LATERAL (SELECT MAX(h.played_at) AS last_played FROM listening_history h
			WHERE h.track_id = t.id) p ON true
		WHERE t.name <> '' AND (l.track_id IS NULL
			OR (l.status IN ('not_found', 'error') AND l.next_attempt_at <= now()))
		ORDER BY (t.id = $2) DESC, (l.track_id IS NULL) DESC, p.last_played DESC NULLS LAST, t.id
		LIMIT $1`, limit, priority)
	if err != nil {
		return nil, fmt.Errorf("tracks needing lyrics: %w", err)
	}
	defer rows.Close()
	out := []domain.LyricsCandidate{}
	for rows.Next() {
		var c domain.LyricsCandidate
		if err := rows.Scan(&c.TrackID, &c.Name, &c.Artist, &c.Album, &c.DurationMS, &c.Status, &c.Attempts); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetLyrics stores a successful lookup (status found or instrumental) and
// clears the retry state.
func (q *Queries) SetLyrics(ctx context.Context, trackID string, l domain.Lyrics) error {
	if l.Status != domain.LyricsFound && l.Status != domain.LyricsInstrumental {
		return fmt.Errorf("set lyrics %s: invalid status %q", trackID, l.Status)
	}
	var lrclibID *int64
	if l.LRCLibID != 0 {
		lrclibID = &l.LRCLibID
	}
	_, err := q.db.Exec(ctx, `
		INSERT INTO lyrics (track_id, status, source, lrclib_id, plain_lyrics, synced_lyrics,
			attempts, last_error, fetched_at, next_attempt_at)
		VALUES ($1, $2, 'lrclib', $3, $4, $5, 0, NULL, now(), NULL)
		ON CONFLICT (track_id) DO UPDATE SET status = EXCLUDED.status, source = EXCLUDED.source,
			lrclib_id = EXCLUDED.lrclib_id, plain_lyrics = EXCLUDED.plain_lyrics,
			synced_lyrics = EXCLUDED.synced_lyrics, attempts = 0, last_error = NULL,
			fetched_at = now(), next_attempt_at = NULL`,
		trackID, l.Status, lrclibID, nullIfEmpty(l.Plain), nullIfEmpty(l.Synced))
	if err != nil {
		return fmt.Errorf("set lyrics %s: %w", trackID, err)
	}
	return nil
}

// DeferLyrics records a lookup that found nothing (not_found) or failed
// (error), to be retried at next. attempts counts lookups in a row with the
// same status (it restarts at 1 when the status changes). Stored lyrics
// (found or instrumental) are never overwritten.
func (q *Queries) DeferLyrics(ctx context.Context, trackID, status string, next time.Time, lastErr string) error {
	if status != domain.LyricsNotFound && status != domain.LyricsError {
		return fmt.Errorf("defer lyrics %s: invalid status %q", trackID, status)
	}
	_, err := q.db.Exec(ctx, `
		INSERT INTO lyrics (track_id, status, source, attempts, last_error, fetched_at, next_attempt_at)
		VALUES ($1, $2, 'lrclib', 1, $4, now(), $3)
		ON CONFLICT (track_id) DO UPDATE SET status = EXCLUDED.status,
			attempts = CASE WHEN lyrics.status = EXCLUDED.status THEN lyrics.attempts + 1 ELSE 1 END,
			last_error = EXCLUDED.last_error, fetched_at = now(), next_attempt_at = EXCLUDED.next_attempt_at
		WHERE lyrics.status IN ('not_found', 'error')`,
		trackID, status, next, nullIfEmpty(lastErr))
	if err != nil {
		return fmt.Errorf("defer lyrics %s: %w", trackID, err)
	}
	return nil
}

// TrackLyrics returns the lyrics record of a track. The synced LRC text is
// included only when withSynced is set. A track with no lyrics row has status
// pending; an unknown track is domain.ErrNotFound.
func (q *Queries) TrackLyrics(ctx context.Context, trackID string, withSynced bool) (TrackLyrics, error) {
	var (
		out            TrackLyrics
		status, source *string
		trStatus       *string
		tr             TrackTranslation
		fresh          bool
		en, vi         []string
		enText, viText *string
		model          *string
		trAttempts     *int32
		trErr          *string
	)
	err := q.db.QueryRow(ctx, `
		SELECT t.id, l.status, l.source, l.lrclib_id, l.plain_lyrics,
			CASE WHEN $2 THEN l.synced_lyrics END,
			COALESCE(l.synced_lyrics, '') <> '', COALESCE(l.attempts, 0), COALESCE(l.last_error, ''),
			l.fetched_at, l.next_attempt_at,
			tr.status, COALESCE(tr.source_hash = `+srcHashSQL+`, false), tr.kind, tr.source_language,
			tr.en_lines, tr.vi_lines, tr.en_text, tr.vi_text, tr.model, tr.attempts, tr.last_error,
			tr.translated_at, tr.next_attempt_at
		FROM tracks t LEFT JOIN lyrics l ON l.track_id = t.id
		LEFT JOIN lyrics_translations tr ON tr.track_id = l.track_id
		WHERE t.id = $1`, trackID, withSynced).Scan(&out.TrackID, &status, &source, &out.LRCLibID,
		&out.Plain, &out.Synced, &out.HasSynced, &out.Attempts, &out.LastError, &out.FetchedAt, &out.NextAttemptAt,
		&trStatus, &fresh, &tr.Kind, &tr.SourceLanguage, &en, &vi, &enText, &viText, &model, &trAttempts, &trErr,
		&tr.TranslatedAt, &tr.NextAttemptAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackLyrics{}, domain.ErrNotFound
	}
	if err != nil {
		return TrackLyrics{}, fmt.Errorf("track lyrics %s: %w", trackID, err)
	}
	out.Status = domain.LyricsPending
	if status != nil {
		out.Status = *status
	}
	out.Source = deref(source)
	if out.Status != domain.LyricsFound {
		out.Plain = nil
		return out, nil
	}
	tr.Status = domain.TranslationPending
	if trStatus != nil {
		tr.Status = *trStatus
		tr.Stale = !fresh
	}
	tr.Model, tr.LastError = deref(model), deref(trErr)
	if trAttempts != nil {
		tr.Attempts = *trAttempts
	}
	tr.EN, tr.VI = joinedTranslation(en, enText), joinedTranslation(vi, viText)
	out.Translation = &tr
	return out, nil
}

// joinedTranslation is a synced translation joined with newlines, or the
// plain one; nil when there is none.
func joinedTranslation(lines []string, text *string) *string {
	if lines != nil {
		s := strings.Join(lines, "\n")
		return &s
	}
	return text
}

// NowLyrics is what the public now-playing lyrics show for one track.
type NowLyrics struct {
	// Status is domain.LyricsFound, domain.LyricsInstrumental,
	// domain.LyricsPending (not looked up yet, or a failed lookup that is
	// retried), or "" (not found: nothing to show).
	Status string
	Synced string // LRC text, "" when there is none
	Plain  string // plain text, "" when there is none
	// Translation is the translation state of found lyrics (nil otherwise).
	Translation *NowTranslation
}

// NowTranslation is the translation of the lyrics in a NowLyrics.
type NowTranslation struct {
	// Status is domain.TranslationDone, domain.TranslationPending (none yet,
	// stale, or a failure that is retried), or "" (skipped, refused or given
	// up: there will be none).
	Status         string
	Kind           string // domain.TranslationSynced or domain.TranslationPlain (done only)
	SourceLanguage string
	EN, VI         []string // synced: one per parsed LRC line; nil = none
	ENText, VIText string   // plain; "" = none
}

// NowLyrics returns what the public now-playing lyrics show for a track:
// found or instrumental lyrics, pending while a lookup is still to come, or
// a zero NowLyrics when none were found. It feeds GET
// /api/v1/now-playing/lyrics, which serves only the track playing right now.
func (q *Queries) NowLyrics(ctx context.Context, trackID string) (NowLyrics, error) {
	var (
		out                  NowLyrics
		status               *string
		retry                bool
		trStatus, kind, lang *string
		enText, viText       *string
		fresh, trRetry       bool
		en, vi               []string
	)
	err := q.db.QueryRow(ctx, `
		SELECT l.status, l.next_attempt_at IS NOT NULL, COALESCE(l.synced_lyrics, ''), COALESCE(l.plain_lyrics, ''),
			tr.status, COALESCE(tr.source_hash = `+srcHashSQL+`, false), tr.next_attempt_at IS NOT NULL,
			tr.kind, tr.source_language, tr.en_lines, tr.vi_lines, tr.en_text, tr.vi_text
		FROM tracks t
		LEFT JOIN lyrics l ON l.track_id = t.id
		LEFT JOIN lyrics_translations tr ON tr.track_id = l.track_id
		WHERE t.id = $1`,
		trackID).Scan(&status, &retry, &out.Synced, &out.Plain, &trStatus, &fresh, &trRetry,
		&kind, &lang, &en, &vi, &enText, &viText)
	if errors.Is(err, pgx.ErrNoRows) {
		return NowLyrics{}, nil
	}
	if err != nil {
		return NowLyrics{}, fmt.Errorf("now lyrics %s: %w", trackID, err)
	}
	switch {
	case status == nil, *status == domain.LyricsError && retry:
		return NowLyrics{Status: domain.LyricsPending}, nil
	case *status == domain.LyricsInstrumental:
		return NowLyrics{Status: domain.LyricsInstrumental}, nil
	case *status != domain.LyricsFound:
		return NowLyrics{}, nil
	}
	out.Status = domain.LyricsFound
	tr := &NowTranslation{Status: translationState(trStatus, fresh, trRetry)}
	if tr.Status == domain.TranslationDone {
		tr.Kind, tr.SourceLanguage = deref(kind), deref(lang)
		tr.EN, tr.VI, tr.ENText, tr.VIText = en, vi, deref(enText), deref(viText)
	}
	out.Translation = tr
	return out, nil
}

// translationState maps a stored translation row to what the public sees:
// done, pending (queued, stale, or a failure that is retried), or "" (not
// queued, skipped, refused, given up). A song with no row was never queued:
// it gets one when it plays, so a missing row also reads as pending.
func translationState(status *string, fresh, retry bool) string {
	switch {
	case status == nil || !fresh:
		return domain.TranslationPending
	case *status == domain.TranslationDone:
		return domain.TranslationDone
	case *status == domain.TranslationPending:
		return domain.TranslationPending
	case *status == domain.TranslationError && retry:
		return domain.TranslationPending
	default:
		return ""
	}
}

// SearchLyrics finds tracks whose lyrics match query (websearch syntax over
// accent- and case-folded text). Exact phrase matches come first. Each match
// has a snippet: the best matching line and the next one.
func (q *Queries) SearchLyrics(ctx context.Context, query string, limit int) ([]LyricsMatch, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	rows, err := q.db.Query(ctx, `
		WITH q AS (
			SELECT websearch_to_tsquery('simple', search_fold($1)) AS wq,
				phraseto_tsquery('simple', search_fold($1)) AS pq,
				replace(plainto_tsquery('simple', search_fold($1))::text, '&', '|')::tsquery AS aq),
		m AS (
			SELECT l.track_id, l.plain_lyrics, (l.search_tsv @@ q.pq) AS phrase,
				COALESCE(l.synced_lyrics, '') <> '' AS has_synced, ts_rank_cd(l.search_tsv, q.wq) AS rank
			FROM q, lyrics l
			WHERE l.status = 'found' AND l.search_tsv @@ q.wq
			ORDER BY phrase DESC, rank DESC, l.track_id
			LIMIT $2)
		SELECT m.phrase, m.has_synced, COALESCE(sn.snippet, ''), `+trackColumns+`
		FROM m CROSS JOIN q
		JOIN tracks t ON t.id = m.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		LEFT JOIN LATERAL (
			SELECT s.n FROM regexp_split_to_table(m.plain_lyrics, E'\n') WITH ORDINALITY AS s(line, n),
				LATERAL to_tsvector('simple', search_fold(s.line)) AS v
			WHERE v @@ q.aq
			ORDER BY (v @@ q.pq) DESC, ts_rank(v, q.aq) DESC, s.n LIMIT 1) best ON true
		LEFT JOIN LATERAL (
			SELECT string_agg(btrim(s.line), ' / ' ORDER BY s.n) AS snippet
			FROM regexp_split_to_table(m.plain_lyrics, E'\n') WITH ORDINALITY AS s(line, n)
			WHERE s.n BETWEEN best.n AND best.n + 1 AND btrim(s.line) <> '') sn ON true
		ORDER BY m.phrase DESC, m.rank DESC, m.track_id`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search lyrics: %w", err)
	}
	defer rows.Close()
	out := []LyricsMatch{}
	for rows.Next() {
		var m LyricsMatch
		var al albumScan
		if err := rows.Scan(append(append([]any{&m.PhraseMatch, &m.HasSynced, &m.Snippet},
			scanTrack(&m.Track)...), al.targets()...)...); err != nil {
			return nil, err
		}
		m.Track.Album = al.album()
		m.Snippet = truncateRunes(m.Snippet, maxSnippet)
		out = append(out, m)
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

// truncateRunes cuts s to at most n runes, ending with "…" when cut.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
