package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

const target = "/api/v1/now-playing/lyrics"

func playing(id string) []byte {
	return snapshot(true, ms(10_000), lyricsNow.Add(-2*time.Second), id, 200_000)
}

// A song still waiting for its lookup gets a pending body (no text), cached
// for the short TTL so it clears soon.
func TestNowLyricsPending(t *testing.T) {
	st := &fakeLyrics{rec: map[string]store.NowLyrics{"p": {Status: domain.LyricsPending}}}
	h := lyricsHandler(st, playing("p"))
	rec, body := getLyrics(t, h, target)
	if rec.Code != http.StatusOK || body == nil || body.Status != "pending" || body.TrackID != "p" ||
		body.Plain != nil || len(body.Lines) != 0 || body.Translation != nil {
		t.Fatalf("pending = %s", rec.Body.String())
	}
	h.now = func() time.Time { return lyricsNow.Add(nowLyricsEmptyTTL - time.Second) }
	getLyrics(t, h, target)
	if st.callCount() != 1 {
		t.Errorf("reads within the pending TTL = %d, want 1", st.callCount())
	}
	h.now = func() time.Time { return lyricsNow.Add(nowLyricsEmptyTTL + time.Second) }
	getLyrics(t, h, target)
	if st.callCount() != 2 {
		t.Errorf("reads after the pending TTL = %d, want 2", st.callCount())
	}
}

func syncedTr(status string, en, vi []string) store.NowLyrics {
	return store.NowLyrics{Status: domain.LyricsFound, Synced: "[00:01.00] một\n[00:02.00]\n[00:03.00] ba",
		Translation: &store.NowTranslation{Status: status, Kind: domain.TranslationSynced, SourceLanguage: "vi",
			Languages: []string{"vi", "en"}, EN: en, VI: vi}}
}

func TestNowLyricsTranslation(t *testing.T) {
	t.Run("done synced", func(t *testing.T) {
		st := &fakeLyrics{rec: map[string]store.NowLyrics{"s": syncedTr(domain.TranslationDone,
			[]string{"one", "", "three"}, nil)}}
		h := lyricsHandler(st, playing("s"))
		h.opts.Translation = true
		_, body := getLyrics(t, h, target)
		tr := body.Translation
		if tr == nil || tr.Status != "done" || tr.SourceLanguage == nil || *tr.SourceLanguage != "vi" ||
			strings.Join(tr.Languages, ",") != "vi,en" ||
			tr.VI != nil || tr.EN == nil || strings.Join(tr.EN.Lines, "|") != "one||three" ||
			tr.Credit.Text != "Translated by Claude" {
			t.Fatalf("translation = %+v", tr)
		}
		if len(tr.EN.Lines) != len(body.Lines) {
			t.Errorf("en lines %d, body lines %d", len(tr.EN.Lines), len(body.Lines))
		}
	})
	t.Run("misaligned language dropped", func(t *testing.T) {
		st := &fakeLyrics{rec: map[string]store.NowLyrics{"s": syncedTr(domain.TranslationDone,
			[]string{"one", "three"}, nil)}}
		h := lyricsHandler(st, playing("s"))
		h.opts.Translation = true
		_, body := getLyrics(t, h, target)
		if body.Translation == nil || body.Translation.EN != nil {
			t.Errorf("misaligned en = %+v", body.Translation)
		}
	})
	t.Run("long line cut", func(t *testing.T) {
		long := strings.Repeat("ư", 400)
		st := &fakeLyrics{rec: map[string]store.NowLyrics{"s": syncedTr(domain.TranslationDone,
			[]string{long, "", "x"}, nil)}}
		h := lyricsHandler(st, playing("s"))
		h.opts.Translation = true
		_, body := getLyrics(t, h, target)
		if n := len([]rune(body.Translation.EN.Lines[0])); n != nowLyricsMaxTrRunes {
			t.Errorf("cut to %d runes", n)
		}
	})
	t.Run("pending uses the short TTL", func(t *testing.T) {
		st := &fakeLyrics{rec: map[string]store.NowLyrics{"s": syncedTr(domain.TranslationPending, nil, nil)}}
		h := lyricsHandler(st, playing("s"))
		h.opts.Translation = true
		_, body := getLyrics(t, h, target)
		if body.Status != "found" || body.Translation == nil || body.Translation.Status != "pending" ||
			body.Translation.EN != nil {
			t.Fatalf("pending translation = %+v", body.Translation)
		}
		h.now = func() time.Time { return lyricsNow.Add(nowLyricsEmptyTTL + time.Second) }
		getLyrics(t, h, target)
		if st.callCount() != 2 {
			t.Errorf("reads = %d, want 2", st.callCount())
		}
	})
	t.Run("plain", func(t *testing.T) {
		st := &fakeLyrics{rec: map[string]store.NowLyrics{"p": {Status: domain.LyricsFound, Plain: "xin chào\n\nbạn",
			Translation: &store.NowTranslation{Status: domain.TranslationDone, Kind: domain.TranslationPlain,
				SourceLanguage: "vi", ENText: "hello\n\nfriend"}}}}
		h := lyricsHandler(st, playing("p"))
		h.opts.Translation = true
		_, body := getLyrics(t, h, target)
		tr := body.Translation
		if tr == nil || tr.EN == nil || tr.EN.Plain == nil || *tr.EN.Plain != "hello\n\nfriend" || tr.VI != nil {
			t.Fatalf("plain translation = %+v", tr)
		}
	})
	t.Run("kind mismatch shows none", func(t *testing.T) {
		rec := syncedTr(domain.TranslationDone, nil, nil)
		rec.Translation.Kind = domain.TranslationPlain
		rec.Translation.ENText = "x"
		st := &fakeLyrics{rec: map[string]store.NowLyrics{"s": rec}}
		h := lyricsHandler(st, playing("s"))
		h.opts.Translation = true
		_, body := getLyrics(t, h, target)
		if body.Translation == nil || body.Translation.EN != nil || body.Translation.VI != nil {
			t.Errorf("mismatch = %+v", body.Translation)
		}
	})
	t.Run("disabled or nothing to show is null", func(t *testing.T) {
		for name, c := range map[string]struct {
			rec store.NowLyrics
			on  bool
		}{
			"disabled": {syncedTr(domain.TranslationDone, []string{"a", "", "b"}, nil), false},
			"skipped":  {syncedTr("", nil, nil), true},
		} {
			st := &fakeLyrics{rec: map[string]store.NowLyrics{"s": c.rec}}
			h := lyricsHandler(st, playing("s"))
			h.opts.Translation = c.on
			rec, body := getLyrics(t, h, target)
			if body.Translation != nil || !strings.Contains(rec.Body.String(), `"translation":null`) {
				t.Errorf("%s: %s", name, rec.Body.String())
			}
		}
	})
}

// With the lyrics fetcher off nothing will look the lyrics up: no pending.
func TestNowLyricsPendingNeedsFetcher(t *testing.T) {
	st := &fakeLyrics{rec: map[string]store.NowLyrics{"p": {Status: domain.LyricsPending}}}
	h := lyricsHandler(st, playing("p"))
	h.opts.LyricsLookup = false
	if _, body := getLyrics(t, h, target); body != nil {
		t.Errorf("body = %+v, want null", body)
	}
}
