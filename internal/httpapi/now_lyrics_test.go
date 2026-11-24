package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// fakeLyrics serves synced text from text, or a whole record from rec.
type fakeLyrics struct {
	mu    sync.Mutex
	text  map[string]string
	rec   map[string]store.NowLyrics
	err   error
	calls int
	delay time.Duration // holds each read open, to overlap concurrent misses
}

func (f *fakeLyrics) NowLyrics(_ context.Context, id string, _ store.TranslationCaps) (store.NowLyrics, error) {
	f.mu.Lock()
	f.calls++
	text := f.text[id]
	rec, ok := f.rec[id]
	f.mu.Unlock()
	time.Sleep(f.delay)
	if f.err != nil {
		return store.NowLyrics{}, f.err
	}
	if ok {
		return rec, nil
	}
	if text == "" {
		return store.NowLyrics{}, nil
	}
	return store.NowLyrics{Status: domain.LyricsFound, Synced: text}, nil
}

func (f *fakeLyrics) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var lyricsNow = time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)

func snapshot(playing bool, progress *int64, fetched time.Time, trackID string, duration int64) []byte {
	type tr struct {
		ID         string `json:"id"`
		DurationMS int64  `json:"duration_ms"`
		Name       string `json:"name"`
	}
	np := map[string]any{"is_playing": playing, "progress_ms": progress, "fetched_at": fetched}
	if trackID != "" {
		np["track"] = tr{ID: trackID, DurationMS: duration, Name: "Song"}
	} else {
		np["track"] = nil
	}
	b, _ := json.Marshal(np)
	return b
}

func ms(v int64) *int64 { return &v }

func lyricsHandler(st *fakeLyrics, snap []byte) *Handler {
	return &Handler{
		opts:     Options{LyricsLookup: true},
		log:      slog.New(slog.DiscardHandler),
		lyrics:   st,
		playback: func(context.Context) ([]byte, error) { return snap, nil },
		now:      func() time.Time { return lyricsNow },
	}
}

func getLyrics(t *testing.T, h *Handler, target string) (*httptest.ResponseRecorder, *nowLyricsBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.nowPlayingLyrics(rec, httptest.NewRequest(http.MethodGet, target, nil))
	var out struct {
		Data *nowLyricsBody `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec, out.Data
}

// manyLines builds n synced lines, one every step ms from 1 s, plus an
// untimed sentinel line that must never be served.
func manyLines(n int, step int64) string {
	var b strings.Builder
	b.WriteString("[ar:Artist]\nUNTIMED-SENTINEL plain words\n")
	for i := range n {
		t := 1000 + int64(i)*step
		fmt.Fprintf(&b, "[%02d:%02d.%02d] line %d\n", t/60000, t/1000%60, t%1000/10, i)
	}
	return b.String()
}

func TestNowLyricsNull(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"t1": manyLines(20, 5000), "empty": "[ar:x]\nno time tags"}}
	fresh := lyricsNow.Add(-2 * time.Second)
	cases := map[string][]byte{
		"null snapshot":  []byte("null"),
		"paused":         snapshot(false, ms(10_000), fresh, "t1", 200_000),
		"no track":       snapshot(true, ms(10_000), fresh, "", 0),
		"no progress":    snapshot(true, nil, fresh, "t1", 200_000),
		"stale":          snapshot(true, ms(10_000), lyricsNow.Add(-2*time.Minute), "t1", 200_000),
		"past duration":  snapshot(true, ms(199_500), fresh, "t1", 200_000),
		"no lyrics":      snapshot(true, ms(10_000), fresh, "other", 200_000),
		"no timed lines": snapshot(true, ms(10_000), fresh, "empty", 200_000),
	}
	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			rec, body := getLyrics(t, lyricsHandler(st, snap), "/api/v1/now-playing/lyrics")
			if rec.Code != http.StatusOK || body != nil || strings.TrimSpace(rec.Body.String()) != `{"data":null}` {
				t.Errorf("got %d %s", rec.Code, rec.Body.String())
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q", cc)
			}
		})
	}
}

func TestNowLyricsFull(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"t1": manyLines(200, 3000)}}
	for _, progress := range []int64{0, 300_000, 598_000} {
		t.Run(fmt.Sprint(progress), func(t *testing.T) {
			snap := snapshot(true, ms(progress), lyricsNow.Add(-2*time.Second), "t1", 900_000)
			rec, body := getLyrics(t, lyricsHandler(st, snap), "/api/v1/now-playing/lyrics")
			if body == nil {
				t.Fatalf("null: %s", rec.Body.String())
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
			}
			if strings.Contains(rec.Body.String(), "SENTINEL") {
				t.Errorf("untimed text served: %s", rec.Body.String())
			}
			if !body.Synced || body.Instrumental || body.Plain != nil || len(body.Lines) != 200 || body.TrackID != "t1" {
				t.Errorf("synced %v instrumental %v plain %v lines %d", body.Synced, body.Instrumental, body.Plain, len(body.Lines))
			}
			if body.Lines[0].Text != "line 0" || body.Lines[199].StartMS != 1000+199*3000 {
				t.Errorf("lines %+v ... %+v", body.Lines[0], body.Lines[199])
			}
			if body.Credit.Text != "Lyrics from LRCLIB" || body.Credit.URL != "https://lrclib.net" {
				t.Errorf("credit = %+v", body.Credit)
			}
		})
	}
}

// Without synced lines the plain text is served; instrumental tracks say so.
func TestNowLyricsPlainAndInstrumental(t *testing.T) {
	st := &fakeLyrics{rec: map[string]store.NowLyrics{
		"plain": {Status: domain.LyricsFound, Synced: "[ar:x]\nno tags", Plain: "  one\ntwo\n "},
		"blank": {Status: domain.LyricsFound, Plain: " \n\t "},
		"inst":  {Status: domain.LyricsInstrumental},
		"long":  {Status: domain.LyricsFound, Plain: strings.Repeat("ế", nowLyricsMaxPlain+50)},
	}}
	snap := func(id string) []byte { return snapshot(true, ms(6000), lyricsNow, id, 200_000) }

	_, body := getLyrics(t, lyricsHandler(st, snap("plain")), "/api/v1/now-playing/lyrics")
	if body == nil || body.Synced || body.Instrumental || body.Plain == nil || *body.Plain != "one\ntwo" || len(body.Lines) != 0 {
		t.Errorf("plain: %+v", body)
	}
	rec, body := getLyrics(t, lyricsHandler(st, snap("inst")), "/api/v1/now-playing/lyrics")
	if body == nil || !body.Instrumental || body.Synced || body.Plain != nil || len(body.Lines) != 0 {
		t.Errorf("instrumental: %s", rec.Body.String())
	}
	_, body = getLyrics(t, lyricsHandler(st, snap("long")), "/api/v1/now-playing/lyrics")
	if body == nil || body.Plain == nil || len([]rune(*body.Plain)) != nowLyricsMaxPlain {
		t.Errorf("long plain not cut")
	}
	if _, body = getLyrics(t, lyricsHandler(st, snap("blank")), "/api/v1/now-playing/lyrics"); body != nil {
		t.Errorf("blank plain: %+v", body)
	}
}

// At most nowLyricsMaxLines lines, and at most nowLyricsMaxTotal runes in all.
func TestNowLyricsCaps(t *testing.T) {
	var big strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&big, "[%02d:%02d.00] %s\n", i/60, i%60, strings.Repeat("a", 150))
	}
	st := &fakeLyrics{text: map[string]string{
		"many": manyLines(nowLyricsMaxLines+500, 100),
		"big":  big.String(),
	}}
	snap := func(id string) []byte { return snapshot(true, ms(6000), lyricsNow, id, 9_000_000) }

	_, body := getLyrics(t, lyricsHandler(st, snap("many")), "/api/v1/now-playing/lyrics")
	if body == nil || len(body.Lines) != nowLyricsMaxLines {
		t.Fatalf("many: %+v", body)
	}
	_, body = getLyrics(t, lyricsHandler(st, snap("big")), "/api/v1/now-playing/lyrics")
	if body == nil || len(body.Lines) != nowLyricsMaxTotal/150 {
		t.Fatalf("big: %v", body)
	}
	total := 0
	for _, l := range body.Lines {
		total += len([]rune(l.Text))
	}
	if total > nowLyricsMaxTotal {
		t.Errorf("total %d runes", total)
	}
}

// Query parameters cannot pick another track.
func TestNowLyricsIgnoresQuery(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"t1": manyLines(10, 5000), "x": manyLines(10, 5000)}}
	snap := snapshot(true, ms(6000), lyricsNow, "t1", 200_000)
	for _, target := range []string{"/api/v1/now-playing/lyrics?track=x", "/api/v1/now-playing/lyrics?id=x&track_id=x"} {
		_, body := getLyrics(t, lyricsHandler(st, snap), target)
		if body == nil || body.TrackID != "t1" {
			t.Errorf("%s served %+v", target, body)
		}
	}
}

func TestNowLyricsMemo(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"t1": manyLines(10, 5000)}}
	h := lyricsHandler(st, snapshot(true, ms(6000), lyricsNow, "t1", 200_000))
	getLyrics(t, h, "/api/v1/now-playing/lyrics")
	getLyrics(t, h, "/api/v1/now-playing/lyrics")
	if st.calls != 1 {
		t.Errorf("store calls = %d, want 1", st.calls)
	}
	h.now = func() time.Time { return lyricsNow.Add(2 * time.Minute) }
	h.playback = func(context.Context) ([]byte, error) {
		return snapshot(true, ms(6000), lyricsNow.Add(2*time.Minute), "t1", 200_000), nil
	}
	getLyrics(t, h, "/api/v1/now-playing/lyrics")
	if st.calls != 2 {
		t.Errorf("store calls after TTL = %d, want 2", st.calls)
	}
}

func TestNowLyricsStoreError(t *testing.T) {
	st := &fakeLyrics{err: errors.New("db down")}
	h := lyricsHandler(st, snapshot(true, ms(6000), lyricsNow, "t1", 200_000))
	rec := httptest.NewRecorder()
	h.nowPlayingLyrics(rec, httptest.NewRequest(http.MethodGet, "/api/v1/now-playing/lyrics", nil))
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("code = %d, Cache-Control = %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

// Concurrent requests on a cache miss share one store read.
func TestNowLyricsCoalescesMisses(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"t1": manyLines(10, 5000)}, delay: 50 * time.Millisecond}
	h := lyricsHandler(st, snapshot(true, ms(6000), lyricsNow, "t1", 200_000))
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			h.nowPlayingLyrics(rec, httptest.NewRequest(http.MethodGet, "/api/v1/now-playing/lyrics", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("code = %d", rec.Code)
			}
		})
	}
	wg.Wait()
	if n := st.callCount(); n != 1 {
		t.Errorf("store calls = %d, want 1", n)
	}
}

// A track without synced lyrics is asked again after 10 s, not a minute.
func TestNowLyricsEmptyTTL(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{}}
	now := lyricsNow
	h := lyricsHandler(st, nil)
	h.now = func() time.Time { return now }
	h.playback = func(context.Context) ([]byte, error) { return snapshot(true, ms(6000), now, "t1", 200_000), nil }
	getLyrics(t, h, "/api/v1/now-playing/lyrics")
	now = now.Add(5 * time.Second)
	getLyrics(t, h, "/api/v1/now-playing/lyrics")
	if n := st.callCount(); n != 1 {
		t.Fatalf("calls within 10 s = %d", n)
	}
	now = now.Add(6 * time.Second)
	st.text["t1"] = manyLines(10, 5000) // the lookup finished meanwhile
	if _, body := getLyrics(t, h, "/api/v1/now-playing/lyrics"); body == nil || st.callCount() != 2 {
		t.Errorf("after 11 s: calls %d, body %v", st.callCount(), body)
	}
}

func TestNowLyricsCutsLongLines(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"t1": "[00:01.00] " + strings.Repeat("ế", 1000)}}
	_, body := getLyrics(t, lyricsHandler(st, snapshot(true, ms(2000), lyricsNow, "t1", 200_000)), "/api/v1/now-playing/lyrics")
	if body == nil || len(body.Lines) != 1 || len([]rune(body.Lines[0].Text)) != nowLyricsMaxRunes {
		t.Fatalf("got %+v", body)
	}
}

// A snapshot stamped in the future (clock skew) still serves the lyrics.
func TestNowLyricsFutureFetchedAt(t *testing.T) {
	st := &fakeLyrics{text: map[string]string{"t1": manyLines(10, 5000)}}
	_, body := getLyrics(t, lyricsHandler(st, snapshot(true, ms(6000), lyricsNow.Add(30*time.Second), "t1", 200_000)), "/api/v1/now-playing/lyrics")
	if body == nil || len(body.Lines) != 10 {
		t.Errorf("got %+v", body)
	}
}
