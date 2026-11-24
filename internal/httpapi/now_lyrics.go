package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/lrc"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// The public now-playing lyrics: lyrics stay admin and MCP only, except for
// the full lyrics of the track playing right now (synced lines when there
// are any, else the plain text). No other track can be asked for.
const (
	nowLyricsMaxLines    = 2000
	nowLyricsMaxRunes    = 200              // per synced line
	nowLyricsMaxPlain    = 20_000           // runes of plain text
	nowLyricsMaxTotal    = 100_000          // runes over all synced lines; later lines are dropped
	nowLyricsMaxAge      = 60 * time.Second // an older snapshot counts as not playing (poller down)
	nowLyricsMemoTTL     = time.Minute      // found lyrics
	nowLyricsEmptyTTL    = 10 * time.Second // no lyrics yet (the lookup may still run)
	lyricsAttribution    = "Lyrics from LRCLIB"
	lyricsAttributionURL = "https://lrclib.net"
)

// nowLyricsStore reads one track's lyrics (implemented by *store.Store).
type nowLyricsStore interface {
	NowLyrics(ctx context.Context, trackID string) (store.NowLyrics, error)
}

type lyricsCreditBody struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type lyricsLine struct {
	StartMS int64  `json:"start_ms"`
	Text    string `json:"text"`
}

// nowLyricsBody is the public lyrics of the track playing now. Synced is
// true when Lines holds timed lines; otherwise Plain holds the text.
// Instrumental tracks have neither.
type nowLyricsBody struct {
	TrackID      string           `json:"track_id"`
	Synced       bool             `json:"synced"`
	Instrumental bool             `json:"instrumental"`
	Lines        []lyricsLine     `json:"lines"`
	Plain        *string          `json:"plain"`
	Credit       lyricsCreditBody `json:"credit"`
}

// lyricsMemo caches the built body of one track, so the database stays off
// the hot path. Empty results are cached for a shorter time. Concurrent
// misses share one store read (flight).
type lyricsMemo struct {
	mu      sync.Mutex
	trackID string
	body    *nowLyricsBody // nil = no lyrics
	at      time.Time
	flight  singleflight.Group
}

func (m *lyricsMemo) get(trackID string, now time.Time) (*nowLyricsBody, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttl := nowLyricsMemoTTL
	if m.body == nil {
		ttl = nowLyricsEmptyTTL
	}
	if m.trackID == trackID && !m.at.IsZero() && now.Sub(m.at) < ttl {
		return m.body, true
	}
	return nil, false
}

func (m *lyricsMemo) put(trackID string, body *nowLyricsBody, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackID, m.body, m.at = trackID, body, now
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
	v, err, _ := h.lyricsCache.flight.Do(trackID, func() (any, error) {
		if body, ok := h.lyricsCache.get(trackID, now); ok {
			return body, nil
		}
		l, err := h.lyrics.NowLyrics(ctx, trackID)
		if err != nil {
			return nil, err
		}
		body := buildLyricsBody(trackID, l)
		h.lyricsCache.put(trackID, body, now)
		return body, nil
	})
	if err != nil {
		return nil, err
	}
	body, _ := v.(*nowLyricsBody)
	return body, nil
}

func buildLyricsBody(trackID string, l store.NowLyrics) *nowLyricsBody {
	out := &nowLyricsBody{
		TrackID: trackID,
		Lines:   []lyricsLine{},
		Credit:  lyricsCreditBody{Text: lyricsAttribution, URL: lyricsAttributionURL},
	}
	switch l.Status {
	case domain.LyricsInstrumental:
		out.Instrumental = true
		return out
	case domain.LyricsFound:
	default:
		return nil
	}
	if lines := lrc.Parse(l.Synced); len(lines) > 0 {
		lines = lines[:min(len(lines), nowLyricsMaxLines)]
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
		return out
	}
	trimmed := strings.TrimSpace(l.Plain)
	if trimmed == "" {
		return nil
	}
	plain := lrc.CutRunes(trimmed, nowLyricsMaxPlain)
	out.Plain = &plain
	return out
}
