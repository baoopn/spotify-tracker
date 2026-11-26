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
	// Waiting is true while a lookup is due soon: not looked up yet, queued,
	// being looked up, re-queued, or a retry within minutes (the admin API
	// also needs the fetcher to be on).
	Waiting bool `json:"waiting"`
	// Queued is true while the track waits for a lookup worker (pending,
	// re-queued, or a retry that is due); LookingUp while one holds it.
	Queued      bool       `json:"queued"`
	LookingUp   bool       `json:"looking_up"`
	RequestedAt *time.Time `json:"requested_at"` // an admin re-queue still waiting
	// ManualAt and ManualBy are set for manual lyrics (source "manual"): the
	// last manual edit and who made it. ReplaceManual is true while a
	// re-queue with replace_manual waits: the next LRCLIB result replaces
	// the manual lyrics.
	ManualAt      *time.Time `json:"manual_at"`
	ManualBy      string     `json:"manual_by"`
	ReplaceManual bool       `json:"replace_manual"`
	// LookupEnabled is set by the admin API: the lyrics fetcher runs
	// (LYRICS_ENABLED), so a re-queue can be served.
	LookupEnabled bool `json:"lookup_enabled"`
	// Translation is the Claude translation state; null unless status is found.
	Translation *TrackTranslation `json:"translation"`
}

// TrackTranslation is the translation of a track's lyrics (admin and MCP).
// Synced translations are joined into one line per parsed LRC line.
type TrackTranslation struct {
	// Status is done, skipped, refused, error, pending (queued), or
	// not_queued (no row: translated when it plays, or re-queue it).
	Status string `json:"status"`
	Stale  bool   `json:"stale"` // the lyrics changed since; it is being redone
	// Waiting is true while the translation will be worked on soon (leased,
	// queued, stale or a retry due within minutes, and no cap reached).
	Waiting        bool     `json:"waiting"`
	Kind           *string  `json:"kind"`
	SourceLanguage *string  `json:"source_language"`
	Languages      []string `json:"languages"` // main first; null if translated before it was stored
	EN             *string  `json:"en"`
	VI             *string  `json:"vi"`
	// Title and Album are the song title and album name in each language;
	// null when neither language has one (or translated before they were
	// stored).
	Title         *TranslatedName `json:"title"`
	Album         *TranslatedName `json:"album"`
	Model         string          `json:"model"`
	Attempts      int32           `json:"attempts"`
	LastError     string          `json:"last_error"`
	TranslatedAt  *time.Time      `json:"translated_at"`
	NextAttemptAt *time.Time      `json:"next_attempt_at"`
}

// TranslatedName is a song title or album name in each target language; a
// null language stays as written (already in that language, or only a name).
type TranslatedName struct {
	EN *string `json:"en"`
	VI *string `json:"vi"`
}

// translatedName is nil when neither language has a translation.
func translatedName(en, vi *string) *TranslatedName {
	if en != nil && *en == "" {
		en = nil
	}
	if vi != nil && *vi == "" {
		vi = nil
	}
	if en == nil && vi == nil {
		return nil
	}
	return &TranslatedName{EN: en, VI: vi}
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

// TrackLyrics returns the lyrics record of a track. The synced LRC text is
// included only when withSynced is set. caps decide whether a queued
// translation is still on its way (Waiting). A track with no lyrics row has status
// pending; an unknown track is domain.ErrNotFound.
func (q *Queries) TrackLyrics(ctx context.Context, trackID string, withSynced bool, caps TranslationCaps) (TrackLyrics, error) {
	var (
		out                                TrackLyrics
		status, source                     *string
		trStatus                           *string
		tr                                 TrackTranslation
		fresh                              bool
		en, vi                             []string
		enText, viText                     *string
		model                              *string
		trAttempts                         *int32
		trErr                              *string
		titleEN, titleVI, albumEN, albumVI *string
	)
	err := q.db.QueryRow(ctx, `
		SELECT t.id, l.status, l.source, l.lrclib_id, l.plain_lyrics,
			CASE WHEN $2 THEN l.synced_lyrics END,
			COALESCE(l.synced_lyrics, '') <> '', COALESCE(l.attempts, 0), COALESCE(l.last_error, ''),
			l.fetched_at, l.next_attempt_at,
			(`+lookupableSQL+`) AND (l.track_id IS NULL OR `+lyricsWaitingSQL+`),
			(`+lookupableSQL+`) AND (l.track_id IS NULL OR `+lyricsQueuedSQL+`),
			COALESCE(`+lyricsLeasedSQL+` AND NOT `+lyricsHeldSQL+`, false), COALESCE(l.requested_at, l.claim_requested_at),
			l.manual_at, COALESCE(l.manual_by, ''), COALESCE(l.replace_manual, false),
			tr.status, COALESCE(tr.source_hash = `+srcHashSQL+`, false), tr.kind, tr.source_language,
			tr.languages, tr.en_lines, tr.vi_lines, tr.en_text, tr.vi_text, tr.model, tr.attempts, tr.last_error,
			tr.translated_at, tr.next_attempt_at, `+trWaitingSQL("false")+`,
			tr.title_en, tr.title_vi, tr.album_en, tr.album_vi
		FROM tracks t LEFT JOIN lyrics l ON l.track_id = t.id
		LEFT JOIN lyrics_translations tr ON tr.track_id = l.track_id
		CROSS JOIN LATERAL (SELECT `+capReachedSQL("$3", "$4")+` AS reached) cap
		WHERE t.id = $1`, trackID, withSynced, caps.Daily, caps.Monthly).Scan(&out.TrackID, &status, &source, &out.LRCLibID,
		&out.Plain, &out.Synced, &out.HasSynced, &out.Attempts, &out.LastError, &out.FetchedAt, &out.NextAttemptAt,
		&out.Waiting, &out.Queued, &out.LookingUp, &out.RequestedAt, &out.ManualAt, &out.ManualBy, &out.ReplaceManual,
		&trStatus, &fresh, &tr.Kind, &tr.SourceLanguage, &tr.Languages, &en, &vi, &enText, &viText, &model, &trAttempts, &trErr,
		&tr.TranslatedAt, &tr.NextAttemptAt, &tr.Waiting, &titleEN, &titleVI, &albumEN, &albumVI)
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
	if out.Status == domain.LyricsPending {
		out.Source = "" // not looked up yet
	}
	if out.Status != domain.LyricsFound {
		out.Plain = nil
		return out, nil
	}
	tr.Status = domain.TranslationNotQueued
	if trStatus != nil {
		tr.Status = *trStatus
		tr.Stale = !fresh
	}
	if tr.Status == domain.TranslationDone && !tr.Stale {
		tr.Waiting = false
	}
	tr.Model, tr.LastError = deref(model), deref(trErr)
	if trAttempts != nil {
		tr.Attempts = *trAttempts
	}
	tr.EN, tr.VI = joinedTranslation(en, enText), joinedTranslation(vi, viText)
	tr.Title, tr.Album = translatedName(titleEN, titleVI), translatedName(albumEN, albumVI)
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
	// domain.LyricsPending (not looked up yet, queued, being looked up, or a
	// not-found or failed lookup retried within minutes), or "" (not found:
	// nothing to show).
	Status string
	// Source is where found or instrumental lyrics come from: "lrclib" or
	// "manual" (added by the admin).
	Source string
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
	Languages      []string // every language in the lyrics, main first; nil = not recorded
	EN, VI         []string // synced: one per parsed LRC line; nil = none
	ENText, VIText string   // plain; "" = none
	// Title and Album are the song title and album name in each language;
	// nil when neither has one.
	Title, Album *TranslatedName
}

// NowLyrics returns what the public now-playing lyrics show for a track:
// found or instrumental lyrics (served even while a fresh lookup is queued),
// pending while a lookup is on its way (no row yet, pending, re-queued,
// leased, or a retry within minutes; see lyricsWaitingSQL; only for a track
// with a title and an artist), or a zero NowLyrics when none were found. caps decide whether a queued translation is still on its way.
// It feeds GET /api/v1/now-playing/lyrics, which serves only the track
// playing right now.
func (q *Queries) NowLyrics(ctx context.Context, trackID string, caps TranslationCaps) (NowLyrics, error) {
	var (
		out                                NowLyrics
		status                             *string
		waitingLookup                      bool
		trStatus, kind, lang               *string
		enText, viText                     *string
		fresh, waiting                     bool
		en, vi, langs                      []string
		titleEN, titleVI, albumEN, albumVI *string
	)
	err := q.db.QueryRow(ctx, `
		SELECT l.status, COALESCE(l.source, ''), (`+lookupableSQL+`) AND COALESCE(l.track_id IS NULL OR `+lyricsWaitingSQL+`, false),
			COALESCE(l.synced_lyrics, ''), COALESCE(l.plain_lyrics, ''),
			tr.status, COALESCE(tr.source_hash = `+srcHashSQL+`, false), `+trWaitingSQL("NOT cap.reached")+`,
			tr.kind, tr.source_language, tr.languages, tr.en_lines, tr.vi_lines, tr.en_text, tr.vi_text,
			tr.title_en, tr.title_vi, tr.album_en, tr.album_vi
		FROM tracks t
		LEFT JOIN lyrics l ON l.track_id = t.id
		LEFT JOIN lyrics_translations tr ON tr.track_id = l.track_id
		CROSS JOIN LATERAL (SELECT `+capReachedSQL("$2", "$3")+` AS reached) cap
		WHERE t.id = $1`,
		trackID, caps.Daily, caps.Monthly).Scan(&status, &out.Source, &waitingLookup, &out.Synced, &out.Plain, &trStatus, &fresh,
		&waiting, &kind, &lang, &langs, &en, &vi, &enText, &viText, &titleEN, &titleVI, &albumEN, &albumVI)
	if errors.Is(err, pgx.ErrNoRows) {
		return NowLyrics{}, nil
	}
	if err != nil {
		return NowLyrics{}, fmt.Errorf("now lyrics %s: %w", trackID, err)
	}
	switch {
	case status == nil && waitingLookup: // no row yet: the queue adds it
		return NowLyrics{Status: domain.LyricsPending}, nil
	case status == nil: // no title or artist: never looked up
		return NowLyrics{}, nil
	case *status == domain.LyricsInstrumental:
		return NowLyrics{Status: domain.LyricsInstrumental, Source: out.Source}, nil
	case *status == domain.LyricsFound:
	case waitingLookup: // pending, queued, being looked up, or retried soon
		return NowLyrics{Status: domain.LyricsPending}, nil
	default:
		return NowLyrics{}, nil
	}
	out.Status = domain.LyricsFound
	tr := &NowTranslation{Status: translationState(trStatus, fresh, waiting)}
	if tr.Status == domain.TranslationDone {
		tr.Kind, tr.SourceLanguage, tr.Languages = deref(kind), deref(lang), langs
		tr.EN, tr.VI, tr.ENText, tr.VIText = en, vi, deref(enText), deref(viText)
		tr.Title, tr.Album = translatedName(titleEN, titleVI), translatedName(albumEN, albumVI)
	}
	out.Translation = tr
	return out, nil
}

// translationState maps a stored translation to what the public sees: done
// (current and finished), pending (it will be worked on soon, see
// trWaitingSQL), or "" (skipped, refused, given up, a retry far away, or
// held back by a reached cap).
func translationState(status *string, fresh, waiting bool) string {
	switch {
	case status != nil && *status == domain.TranslationDone && fresh:
		return domain.TranslationDone
	case waiting:
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
