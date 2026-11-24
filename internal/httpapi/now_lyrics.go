package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/baoopn/spotify-tracker/internal/lrc"
)

// The public lyrics window: lyrics stay admin and MCP only, except for this
// short run of synced lines of the track playing right now. It never carries
// plain lyrics or the full synced text.
const (
	nowLyricsAhead       = 30 * time.Second
	nowLyricsMaxLines    = 10
	nowLyricsMaxRunes    = 200
	nowLyricsMaxAge      = 60 * time.Second // an older snapshot counts as not playing (poller down)
	nowLyricsMemoTTL     = time.Minute      // found lines
	nowLyricsEmptyTTL    = 10 * time.Second // no synced lyrics yet (the lookup may still run)
	lyricsAttribution    = "Lyrics from LRCLIB"
	lyricsAttributionURL = "https://lrclib.net"
)

// syncedLyricsStore reads synced LRC text (implemented by *store.Store).
type syncedLyricsStore interface {
	SyncedLyrics(ctx context.Context, trackID string) (string, error)
}

// lyricsMemo caches the parsed lines of one track, so the database stays off
// the hot path of a poll every few seconds. Empty results are cached for a
// shorter time. Concurrent misses share one store read (flight).
type lyricsMemo struct {
	mu      sync.Mutex
	trackID string
	lines   []lrc.Line
	at      time.Time
	flight  singleflight.Group
}

func (m *lyricsMemo) get(trackID string, now time.Time) ([]lrc.Line, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttl := nowLyricsMemoTTL
	if len(m.lines) == 0 {
		ttl = nowLyricsEmptyTTL
	}
	if m.trackID == trackID && !m.at.IsZero() && now.Sub(m.at) < ttl {
		return m.lines, true
	}
	return nil, false
}

func (m *lyricsMemo) put(trackID string, lines []lrc.Line, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackID, m.lines, m.at = trackID, lines, now
}

type lyricsCreditBody struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type lyricsWindowLine struct {
	StartMS int64  `json:"start_ms"`
	Text    string `json:"text"`
}

type nowLyricsBody struct {
	TrackID    string             `json:"track_id"`
	PositionMS int64              `json:"position_ms"`
	At         time.Time          `json:"at"`
	FromMS     int64              `json:"from_ms"`
	UntilMS    *int64             `json:"until_ms"`
	Lines      []lyricsWindowLine `json:"lines"`
	Credit     lyricsCreditBody   `json:"credit"`
}

// playbackSnapshot is the part of the now-playing snapshot the window needs.
type playbackSnapshot struct {
	IsPlaying  bool      `json:"is_playing"`
	ProgressMS *int64    `json:"progress_ms"`
	FetchedAt  time.Time `json:"fetched_at"`
	Track      *struct {
		ID         string `json:"id"`
		DurationMS int64  `json:"duration_ms"`
	} `json:"track"`
}

// GET /api/v1/now-playing/lyrics — a short window of synced lines of the
// track playing now. Takes no parameters; {"data":null} when nothing fits.
func (h *Handler) nowPlayingLyrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	body, err := h.nowLyricsWindow(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, itemResponse[*nowLyricsBody]{Data: body})
}

func (h *Handler) nowLyricsWindow(ctx context.Context) (*nowLyricsBody, error) {
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
	pos := *np.ProgressMS + max(0, now.Sub(np.FetchedAt).Milliseconds())
	pos = max(0, pos)
	if d := np.Track.DurationMS; d > 0 && pos > d {
		return nil, nil // the track has ended; the next snapshot names the new one
	}

	lines, err := h.lyricLines(ctx, np.Track.ID, now)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}

	win := lrc.Cut(lines, pos, lrc.WindowOptions{
		Ahead: nowLyricsAhead, MaxLines: nowLyricsMaxLines, MaxRunes: nowLyricsMaxRunes,
	})
	out := &nowLyricsBody{
		TrackID: np.Track.ID, PositionMS: pos, At: now.UTC(),
		FromMS: win.FromMS, UntilMS: win.UntilMS,
		Lines:  make([]lyricsWindowLine, len(win.Lines)),
		Credit: lyricsCreditBody{Text: lyricsAttribution, URL: lyricsAttributionURL},
	}
	for i, l := range win.Lines {
		out.Lines[i] = lyricsWindowLine{StartMS: l.StartMS, Text: l.Text}
	}
	return out, nil
}

// lyricLines returns the parsed synced lines of trackID from the memo, or
// reads them once for all concurrent callers.
func (h *Handler) lyricLines(ctx context.Context, trackID string, now time.Time) ([]lrc.Line, error) {
	if lines, ok := h.lyricsCache.get(trackID, now); ok {
		return lines, nil
	}
	v, err, _ := h.lyricsCache.flight.Do(trackID, func() (any, error) {
		if lines, ok := h.lyricsCache.get(trackID, now); ok {
			return lines, nil
		}
		synced, err := h.lyrics.SyncedLyrics(ctx, trackID)
		if err != nil {
			return nil, err
		}
		lines := lrc.Parse(synced)
		h.lyricsCache.put(trackID, lines, now)
		return lines, nil
	})
	if err != nil {
		return nil, err
	}
	lines, _ := v.([]lrc.Line)
	return lines, nil
}
