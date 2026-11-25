package httpapi

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

const (
	// lyricsMemoSlots is how many tracks the memo holds: the song playing
	// now and the upcoming songs warmed by queue prefetch.
	lyricsMemoSlots = 8
	// lyricsWarmTTL keeps a warmed body until the song is likely to start.
	lyricsWarmTTL = 10 * time.Minute
)

// lyricsMemo caches built bodies per track, so the database stays off the
// hot path. A body that is pending lives nowLyricsEmptyTTL; others their
// own TTL. Concurrent misses share one store read (flight).
type lyricsMemo struct {
	mu      sync.Mutex
	entries map[string]memoEntry
	flight  singleflight.Group
}

type memoEntry struct {
	body *nowLyricsBody // nil = no lyrics
	at   time.Time
	ttl  time.Duration
}

// get returns a fresh body for a request. A warmed entry (long TTL) falls
// back to the normal TTL from its first request on, so the song playing
// now is re-read about once a minute.
func (m *lyricsMemo) get(trackID string, now time.Time) (*nowLyricsBody, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.lookup(trackID, now)
	if !ok {
		return nil, false
	}
	if e.ttl > nowLyricsMemoTTL {
		e.at, e.ttl = now, nowLyricsMemoTTL
		m.entries[trackID] = e
	}
	return e.body, true
}

// fresh reports whether a fresh entry exists, without serving it.
func (m *lyricsMemo) fresh(trackID string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.lookup(trackID, now)
	return ok
}

func (m *lyricsMemo) lookup(trackID string, now time.Time) (memoEntry, bool) {
	e, ok := m.entries[trackID]
	if !ok {
		return e, false
	}
	ttl := e.ttl
	if e.body.pending() {
		ttl = nowLyricsEmptyTTL
	}
	return e, now.Sub(e.at) < ttl
}

// put stores a body; past lyricsMemoSlots the oldest other entry goes.
func (m *lyricsMemo) put(trackID string, body *nowLyricsBody, now time.Time, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = map[string]memoEntry{}
	}
	m.entries[trackID] = memoEntry{body: body, at: now, ttl: ttl}
	for len(m.entries) > lyricsMemoSlots {
		oldest := ""
		for id, e := range m.entries {
			if id != trackID && (oldest == "" || e.at.Before(m.entries[oldest].at)) {
				oldest = id
			}
		}
		delete(m.entries, oldest)
	}
}

// buildBody turns a store record into the public body.
func (h *Handler) buildBody(trackID string, l store.NowLyrics) *nowLyricsBody {
	if l.Status == domain.LyricsPending && !h.opts.LyricsLookup {
		l = store.NowLyrics{} // nothing will look the lyrics up
	}
	return buildLyricsBody(trackID, l, h.opts.Translation)
}

// warmLyrics builds and caches the body of an upcoming track, so its lyrics
// are served from memory the moment it starts. It runs in the same flight
// as requests, so it never overwrites a body built meanwhile. A body still
// pending is not kept (it would only live seconds). force rebuilds a fresh
// entry (a translation just finished).
func (h *Handler) warmLyrics(ctx context.Context, trackID string, force bool) {
	if h.lyrics == nil || trackID == "" {
		return
	}
	now := h.clock()
	if !force && h.lyricsCache.fresh(trackID, now) {
		return
	}
	_, _, _ = h.lyricsCache.flight.Do(trackID, func() (any, error) {
		if !force && h.lyricsCache.fresh(trackID, now) {
			return nil, nil
		}
		l, err := h.lyrics.NowLyrics(ctx, trackID, h.opts.TranslationCaps)
		if err != nil {
			return nil, err
		}
		body := h.buildBody(trackID, l)
		if body != nil && !body.pending() {
			h.lyricsCache.put(trackID, body, now, lyricsWarmTTL)
		}
		return body, nil
	})
}

// LyricsWarmer lets queue prefetch warm the public now-playing lyrics cache
// for upcoming tracks. Pass one in Options; NewRouter connects it. The zero
// value (or one not passed to a router) does nothing.
type LyricsWarmer struct {
	mu sync.Mutex
	h  *Handler
}

// Warm caches the public lyrics body of trackID (best effort); force
// rebuilds an entry that is already cached.
func (w *LyricsWarmer) Warm(ctx context.Context, trackID string, force bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	h := w.h
	w.mu.Unlock()
	if h != nil {
		h.warmLyrics(ctx, trackID, force)
	}
}

func (w *LyricsWarmer) attach(h *Handler) {
	w.mu.Lock()
	w.h = h
	w.mu.Unlock()
}
