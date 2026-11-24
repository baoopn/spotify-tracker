package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

type fakeLyrics struct {
	id         string
	withSynced bool
	q          string
	limit      int
	err        error
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
