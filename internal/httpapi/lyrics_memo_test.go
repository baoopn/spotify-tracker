package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// Warming caches an upcoming track, so its first request reads no database,
// and the warmed body outlives the normal one-minute TTL.
func TestLyricsWarm(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"next": manyLines(10, 5000)}}
	h := lyricsHandler(st, snapshot(true, ms(6000), lyricsNow.Add(5*time.Minute), "next", 200_000))
	w := &LyricsWarmer{}
	w.attach(h)
	w.Warm(context.Background(), "next", false)
	if st.callCount() != 1 {
		t.Fatalf("warm calls = %d", st.callCount())
	}
	h.now = func() time.Time { return lyricsNow.Add(5 * time.Minute) }
	_, body := getLyrics(t, h, "/api/v1/now-playing/lyrics")
	if body == nil || body.TrackID != "next" || len(body.Lines) != 10 {
		t.Fatalf("body = %+v", body)
	}
	if st.callCount() != 1 {
		t.Errorf("store read after warm: %d", st.callCount())
	}
}

// A pending body is not warmed: it would only live seconds.
func TestLyricsWarmSkipsPending(t *testing.T) {
	st := &fakeLyrics{rec: map[string]store.NowLyrics{"p": {Status: domain.LyricsPending}}}
	h := lyricsHandler(st, nil)
	(&LyricsWarmer{h: h}).Warm(context.Background(), "p", false)
	if _, ok := h.lyricsCache.get("p", lyricsNow); ok {
		t.Error("pending body was cached")
	}
	var nilWarmer *LyricsWarmer
	nilWarmer.Warm(context.Background(), "p", false) // no panic
	(&LyricsWarmer{}).Warm(context.Background(), "p", false)
}

// The memo keeps lyricsMemoSlots tracks and never evicts the one just put.
func TestLyricsMemoEviction(t *testing.T) {
	var m lyricsMemo
	body := &nowLyricsBody{Status: domain.LyricsFound}
	for i := range lyricsMemoSlots {
		m.put(fmt.Sprint("t", i), body, lyricsNow.Add(time.Duration(i)*time.Second), time.Hour)
	}
	m.put("new", body, lyricsNow.Add(-time.Hour), time.Hour) // oldest timestamp of all
	if len(m.entries) != lyricsMemoSlots {
		t.Fatalf("len = %d", len(m.entries))
	}
	if _, ok := m.entries["new"]; !ok {
		t.Error("the entry just put was evicted")
	}
	if _, ok := m.entries["t0"]; ok {
		t.Error("the oldest other entry was kept")
	}
}

// Per-entry TTL: a normal body expires after its TTL; pending bodies use
// the short TTL.
func TestLyricsMemoTTL(t *testing.T) {
	var m lyricsMemo
	m.put("a", &nowLyricsBody{Status: domain.LyricsFound}, lyricsNow, time.Minute)
	m.put("p", &nowLyricsBody{Status: domain.LyricsPending}, lyricsNow, time.Hour)
	if _, ok := m.get("a", lyricsNow.Add(59*time.Second)); !ok {
		t.Error("a expired early")
	}
	if _, ok := m.get("a", lyricsNow.Add(time.Minute)); ok {
		t.Error("a outlived its TTL")
	}
	if _, ok := m.get("p", lyricsNow.Add(nowLyricsEmptyTTL)); ok {
		t.Error("pending body outlived the short TTL")
	}
}

// Once served, a warmed entry falls back to the normal TTL; a forced warm
// rebuilds a cached entry (a translation finished).
func TestLyricsWarmServedAndForced(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"next": manyLines(10, 5000)}}
	h := lyricsHandler(st, nil)
	w := &LyricsWarmer{h: h}
	w.Warm(context.Background(), "next", false)
	if _, ok := h.lyricsCache.get("next", lyricsNow); !ok { // first request at lyricsNow
		t.Fatal("warmed entry missing")
	}
	if _, ok := h.lyricsCache.get("next", lyricsNow.Add(nowLyricsMemoTTL+time.Second)); ok {
		t.Error("a served warmed entry kept the long TTL")
	}
	w.Warm(context.Background(), "next", false)
	n := st.callCount()
	w.Warm(context.Background(), "next", false) // fresh: no read
	if st.callCount() != n {
		t.Error("a fresh entry was read again")
	}
	w.Warm(context.Background(), "next", true)
	if st.callCount() != n+1 {
		t.Error("a forced warm did not rebuild")
	}
}
