package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

type fakeLyrics struct {
	id         string
	withSynced bool
	q          string
	limit      int
	err        error
	requeued   []string
	noLyrics   bool
}

func (f *fakeLyrics) TranslationQueueSummary(context.Context, time.Time) (store.TranslationQueue, error) {
	return store.TranslationQueue{Queued: 2, InProgress: 1, Done: 5, GivenUp: 1, CallsToday: 3, CallsMonth: 40}, f.err
}

func (f *fakeLyrics) RequeueTranslation(_ context.Context, id string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.requeued = append(f.requeued, id)
	return !f.noLyrics, nil
}

func (f *fakeLyrics) TrackLyrics(_ context.Context, id string, withSynced bool) (store.TrackLyrics, error) {
	f.id, f.withSynced = id, withSynced
	if f.err != nil {
		return store.TrackLyrics{}, f.err
	}
	p := "line"
	return store.TrackLyrics{TrackID: id, Status: domain.LyricsFound, Plain: &p}, nil
}

func (f *fakeLyrics) SearchLyrics(_ context.Context, q string, limit int) ([]store.LyricsMatch, error) {
	f.q, f.limit = q, limit
	return []store.LyricsMatch{{Track: domain.TrackView{ID: "t1"}, Snippet: "a / b", PhraseMatch: true}}, nil
}

func lyricsAPI(f *fakeLyrics) *API {
	return &API{Deps: Deps{Log: slog.New(slog.DiscardHandler)}, lyrics: f}
}

func getLyrics(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func lyricsErrCode(rec *httptest.ResponseRecorder) string {
	var body struct {
		Error apiError `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error.Code
}

func TestTrackLyricsHandler(t *testing.T) {
	f := &fakeLyrics{}
	h := ReadOnlyHandler(lyricsAPI(f))
	rec := getLyrics(h, "/tracks/abc/lyrics?synced=true")
	if rec.Code != http.StatusOK || f.id != "abc" || !f.withSynced {
		t.Fatalf("code %d, id %q, synced %v", rec.Code, f.id, f.withSynced)
	}
	if !strings.Contains(rec.Body.String(), `"status":"found"`) || !strings.Contains(rec.Body.String(), `"plain":"line"`) {
		t.Errorf("body = %s", rec.Body)
	}
	for v, want := range map[string]bool{"": false, "false": false, "0": false, "1": true, "TRUE": true} {
		getLyrics(h, "/tracks/abc/lyrics?synced="+v)
		if f.withSynced != want {
			t.Errorf("synced=%q gave %v", v, f.withSynced)
		}
	}
	if rec := getLyrics(h, "/tracks/abc/lyrics?synced=yes"); rec.Code != http.StatusBadRequest || lyricsErrCode(rec) != "invalid_synced" {
		t.Errorf("synced=yes = %d %s", rec.Code, rec.Body)
	}
	f.err = domain.ErrNotFound
	if rec := getLyrics(h, "/tracks/nope/lyrics"); rec.Code != http.StatusNotFound || lyricsErrCode(rec) != "not_found" {
		t.Errorf("unknown track = %d %s", rec.Code, rec.Body)
	}
}

func TestSearchLyricsHandler(t *testing.T) {
	f := &fakeLyrics{}
	h := ReadOnlyHandler(lyricsAPI(f))
	rec := getLyrics(h, "/lyrics/search?q=%20anh%20m%C6%A1%20")
	if rec.Code != http.StatusOK || f.q != "anh mơ" || f.limit != 10 {
		t.Fatalf("code %d, q %q, limit %d", rec.Code, f.q, f.limit)
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Data) != 1 ||
		body.Data[0]["snippet"] != "a / b" || body.Data[0]["phrase_match"] != true || body.Data[0]["track"] == nil {
		t.Fatalf("body = %s", rec.Body)
	}
	if _, ok := body.Data[0]["has_synced"]; !ok {
		t.Error("has_synced missing")
	}
	for _, q := range []string{"", "a", "%20%20", strings.Repeat("a", 201), "!!!", "%FF%FEab", "ab%00cd"} {
		if rec := getLyrics(h, "/lyrics/search?q="+q); rec.Code != http.StatusBadRequest || lyricsErrCode(rec) != "invalid_query" {
			t.Errorf("q=%q = %d %s", q, rec.Code, rec.Body)
		}
	}
	if rec := getLyrics(h, "/lyrics/search?q="+strings.Repeat("%C6%A1", 200)); rec.Code != http.StatusOK {
		t.Errorf("200 runes = %d", rec.Code)
	}
	for _, l := range []string{"0", "51", "x"} {
		if rec := getLyrics(h, "/lyrics/search?q=ab&limit="+l); rec.Code != http.StatusBadRequest || lyricsErrCode(rec) != "invalid_limit" {
			t.Errorf("limit=%s = %d", l, rec.Code)
		}
	}
	getLyrics(h, "/lyrics/search?q=ab&limit=50")
	if f.limit != 50 {
		t.Errorf("limit = %d", f.limit)
	}
}

func TestTranslationQueueHandler(t *testing.T) {
	a := lyricsAPI(&fakeLyrics{})
	a.Translation = TranslationInfo{Enabled: true, DailyCap: 100, MonthlyCap: 1000}
	rec := getLyrics(ReadOnlyHandler(a), "/lyrics/translation-queue")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	for k, want := range map[string]float64{"queued": 2, "in_progress": 1, "done": 5, "given_up": 1,
		"calls_today": 3, "calls_month": 40, "daily_cap": 100, "monthly_cap": 1000} {
		if body[k] != want {
			t.Errorf("%s = %v, want %v", k, body[k], want)
		}
	}
	if body["enabled"] != true {
		t.Errorf("enabled = %v", body["enabled"])
	}
}

// Re-queue is the one write route MCP can reach: it nudges the worker and
// is audited.
func TestRequeueTranslationHandler(t *testing.T) {
	f := &fakeLyrics{}
	a := lyricsAPI(f)
	aud := &fakeAuditor{}
	a.auditor = aud
	nudged := 0
	a.Translation.Nudge = func() { nudged++ }
	h := ReadOnlyHandler(a)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tracks/t9/translation/requeue", nil))
	if rec.Code != http.StatusAccepted || len(f.requeued) != 1 || f.requeued[0] != "t9" || nudged != 1 {
		t.Fatalf("requeue: %d %s requeued=%v nudged=%d", rec.Code, rec.Body.String(), f.requeued, nudged)
	}
	if len(aud.calls) != 1 || aud.calls[0].action != "translation.requeued" {
		t.Errorf("audit = %+v", aud.calls)
	}

	f.noLyrics = true
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tracks/t9/translation/requeue", nil))
	if rec.Code != http.StatusNotFound || lyricsErrCode(rec) != "no_lyrics" || nudged != 1 {
		t.Errorf("no lyrics: %d %s", rec.Code, rec.Body.String())
	}

	// Other writes stay off the MCP handler.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tracks/t9/enrich", nil))
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Errorf("enrich via MCP handler = %d", rec.Code)
	}
}
