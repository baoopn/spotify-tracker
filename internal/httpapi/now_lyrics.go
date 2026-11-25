package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/lrc"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// The public now-playing lyrics: lyrics stay admin and MCP only, except for
// the full lyrics of the track playing right now (synced lines when there
// are any, else the plain text) and their Claude translation. No other track
// can be asked for.
const (
	nowLyricsMaxLines    = 2000
	nowLyricsMaxRunes    = 200              // per synced line
	nowLyricsMaxPlain    = 20_000           // runes of plain text
	nowLyricsMaxTotal    = 100_000          // runes over all synced lines; later lines are dropped
	nowLyricsMaxAge      = 60 * time.Second // an older snapshot counts as not playing (poller down)
	nowLyricsMemoTTL     = time.Minute      // found lyrics
	nowLyricsEmptyTTL    = 10 * time.Second // nothing found yet, or lyrics / translation pending
	nowLyricsMaxTrRunes  = 300              // per translated synced line
	nowLyricsMaxTrPlain  = 30_000           // runes of a plain translation
	lyricsAttribution    = "Lyrics from LRCLIB"
	lyricsAttributionURL = "https://lrclib.net"
	// manualAttribution credits lyrics the owner added by hand (no link);
	// the public site names the owner "Bao" ("What Bao is listening to").
	manualAttribution = "Lyrics added by Bao"
	translationCredit = "Translated by Claude"
)

// Body statuses.
const (
	nowLyricsFound        = "found"
	nowLyricsInstrumental = "instrumental"
	nowLyricsPending      = "pending" // the lookup is still to come: ask again soon
)

// nowLyricsStore reads one track's lyrics (implemented by *store.Store).
type nowLyricsStore interface {
	NowLyrics(ctx context.Context, trackID string, caps store.TranslationCaps) (store.NowLyrics, error)
}

// lyricsCreditBody credits the lyrics source; URL is null when there is no
// link (lyrics added by hand).
type lyricsCreditBody struct {
	Text string  `json:"text"`
	URL  *string `json:"url"`
}

// lyricsCredit is the credit for lyrics from source ("manual" or LRCLIB).
// A pending body (no source yet) credits LRCLIB, which looks it up.
func lyricsCredit(source string) lyricsCreditBody {
	if source == "manual" {
		return lyricsCreditBody{Text: manualAttribution}
	}
	u := lyricsAttributionURL
	return lyricsCreditBody{Text: lyricsAttribution, URL: &u}
}

type lyricsLine struct {
	StartMS int64  `json:"start_ms"`
	Text    string `json:"text"`
}

// nowLyricsBody is the public lyrics of the track playing now. Status is
// found (Synced is true when Lines holds timed lines; otherwise Plain holds
// the text), instrumental (no text) or pending (looked up soon; no text).
type nowLyricsBody struct {
	TrackID      string           `json:"track_id"`
	Status       string           `json:"status"`
	Synced       bool             `json:"synced"`
	Instrumental bool             `json:"instrumental"`
	Lines        []lyricsLine     `json:"lines"`
	Plain        *string          `json:"plain"`
	Credit       lyricsCreditBody `json:"credit"`
	// Translation is null when translation is off, for instrumental or
	// pending lyrics, and when there will be none (skipped, refused).
	Translation *translationBody `json:"translation"`
}

// translationBody is the Claude translation of found lyrics. While pending
// the languages are null. A null language means the song is in that
// language (or the translation did not line up).
type translationBody struct {
	Status         string                `json:"status"` // pending or done
	SourceLanguage *string               `json:"source_language"`
	EN             *translationText      `json:"en"`
	VI             *translationText      `json:"vi"`
	Credit         translationCreditBody `json:"credit"`
}

// translationText is one language: Lines lines up with the body's Lines
// (same length, "" for empty lines) for synced lyrics; Plain for plain ones.
type translationText struct {
	Lines []string `json:"lines"`
	Plain *string  `json:"plain"`
}

type translationCreditBody struct {
	Text string `json:"text"`
}

// pending reports whether the body should be asked for again soon.
func (b *nowLyricsBody) pending() bool {
	return b == nil || b.Status == nowLyricsPending ||
		(b.Translation != nil && b.Translation.Status == domain.TranslationPending)
}

// playbackSnapshot is the part of the now-playing snapshot the endpoint needs.
type playbackSnapshot struct {
	IsPlaying  bool      `json:"is_playing"`
	ProgressMS *int64    `json:"progress_ms"`
	FetchedAt  time.Time `json:"fetched_at"`
	Track      *struct {
		ID         string `json:"id"`
		DurationMS int64  `json:"duration_ms"`
	} `json:"track"`
}

// GET /api/v1/now-playing/lyrics — the lyrics of the track playing now.
// Takes no parameters; {"data":null} when nothing is playing or there are
// no lyrics.
func (h *Handler) nowPlayingLyrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	body, err := h.nowLyrics(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, itemResponse[*nowLyricsBody]{Data: body})
}

func (h *Handler) nowLyrics(ctx context.Context) (*nowLyricsBody, error) {
	if h.lyrics == nil {
		return nil, nil
	}
	read := h.playback
	if read == nil {
		read = h.nowPlayingJSON
	}
	raw, err := read(ctx)
	if err != nil {
		return nil, err
	}
	var np *playbackSnapshot
	if err := json.Unmarshal(raw, &np); err != nil {
		return nil, fmt.Errorf("decode now playing: %w", err)
	}
	now := h.clock()
	if np == nil || np.Track == nil || np.Track.ID == "" || !np.IsPlaying || np.ProgressMS == nil {
		return nil, nil
	}
	if now.Sub(np.FetchedAt) > nowLyricsMaxAge {
		return nil, nil
	}
	pos := max(0, *np.ProgressMS+max(0, now.Sub(np.FetchedAt).Milliseconds()))
	if d := np.Track.DurationMS; d > 0 && pos > d {
		return nil, nil // the track has ended; the next snapshot names the new one
	}
	return h.lyricsBody(ctx, np.Track.ID, now)
}

// lyricsBody returns the body for trackID from the memo, or builds it once
// for all concurrent callers.
func (h *Handler) lyricsBody(ctx context.Context, trackID string, now time.Time) (*nowLyricsBody, error) {
	if body, ok := h.lyricsCache.get(trackID, now); ok {
		return body, nil
	}
	load := func() (*nowLyricsBody, error) {
		if body, ok := h.lyricsCache.get(trackID, now); ok {
			return body, nil
		}
		l, err := h.lyrics.NowLyrics(ctx, trackID, h.opts.TranslationCaps)
		if err != nil {
			return nil, err
		}
		body := h.buildBody(trackID, l)
		h.lyricsCache.put(trackID, body, now, nowLyricsMemoTTL)
		return body, nil
	}
	v, err, _ := h.lyricsCache.flight.Do(trackID, func() (any, error) { return load() })
	if err != nil {
		return nil, err
	}
	if w, ok := v.(warmResult); ok {
		// Joined a warm-up of the lyrics cache: use its body, or load with
		// this request's context when the warm failed.
		if !w.ok {
			return load()
		}
		if body, ok := h.lyricsCache.get(trackID, now); ok {
			return body, nil // served: the warm TTL falls back to the normal one
		}
		return w.body, nil
	}
	body, _ := v.(*nowLyricsBody)
	return body, nil
}

func buildLyricsBody(trackID string, l store.NowLyrics, translation bool) *nowLyricsBody {
	out := &nowLyricsBody{
		TrackID: trackID,
		Status:  l.Status,
		Lines:   []lyricsLine{},
		Credit:  lyricsCredit(l.Source),
	}
	switch l.Status {
	case domain.LyricsInstrumental:
		out.Instrumental = true
		return out
	case domain.LyricsPending:
		return out
	case domain.LyricsFound:
	default:
		return nil
	}
	if lines := lrc.Parse(l.Synced); len(lines) > 0 {
		parsed := min(len(lines), nowLyricsMaxLines)
		lines = lines[:parsed]
		out.Synced = true
		out.Lines = make([]lyricsLine, 0, len(lines))
		total := 0
		for _, ln := range lines {
			text := lrc.CutRunes(ln.Text, nowLyricsMaxRunes)
			total += utf8.RuneCountInString(text)
			if total > nowLyricsMaxTotal {
				break
			}
			out.Lines = append(out.Lines, lyricsLine{StartMS: ln.StartMS, Text: text})
		}
		if translation {
			out.Translation = buildTranslation(l.Translation, domain.TranslationSynced, parsed, len(out.Lines))
		}
		return out
	}
	trimmed := strings.TrimSpace(l.Plain)
	if trimmed == "" {
		return nil
	}
	plain := lrc.CutRunes(trimmed, nowLyricsMaxPlain)
	out.Plain = &plain
	if translation {
		out.Translation = buildTranslation(l.Translation, domain.TranslationPlain, 0, 0)
	}
	return out
}

// buildTranslation shapes the stored translation for a body of kind. For
// synced lyrics a language is kept only when it has one line per parsed LRC
// line (parsed), and is cut to the served lines (served).
func buildTranslation(t *store.NowTranslation, kind string, parsed, served int) *translationBody {
	if t == nil || (t.Status != domain.TranslationDone && t.Status != domain.TranslationPending) {
		return nil
	}
	out := &translationBody{Status: t.Status, Credit: translationCreditBody{Text: translationCredit}}
	if t.Status == domain.TranslationPending {
		return out
	}
	if t.Kind != kind {
		return out // translated from a different form of the lyrics: show none
	}
	if t.SourceLanguage != "" {
		lang := t.SourceLanguage
		out.SourceLanguage = &lang
	}
	text := func(lines []string, plain string) *translationText {
		if kind == domain.TranslationSynced {
			if len(lines) != parsed {
				return nil
			}
			cut := make([]string, served)
			for i := range cut {
				cut[i] = lrc.CutRunes(lines[i], nowLyricsMaxTrRunes)
			}
			return &translationText{Lines: cut}
		}
		if plain = strings.TrimSpace(plain); plain == "" {
			return nil
		}
		p := lrc.CutRunes(plain, nowLyricsMaxTrPlain)
		return &translationText{Lines: []string{}, Plain: &p}
	}
	out.EN = text(t.EN, t.ENText)
	out.VI = text(t.VI, t.VIText)
	return out
}
