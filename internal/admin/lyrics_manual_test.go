package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/lrc"
)

// manualFake is the manual lyrics part of fakeLyrics.
type manualFake struct {
	manual      bool // the track has manual lyrics (re-queue 409, clear works)
	replaced    bool // the last re-queue had replace_manual
	duration    int64
	set         []domain.Lyrics
	setBy       string
	unchanged   bool // SetManualLyrics reports stored = false
	retranslate bool
	cleared     []string
}

func (f *manualFake) SetManualLyrics(_ context.Context, id string, l domain.Lyrics, by string) (bool, bool, error) {
	if id == "unknown" {
		return false, false, domain.ErrNotFound
	}
	f.set, f.setBy = append(f.set, l), by
	return !f.unchanged, f.retranslate, nil
}

func (f *manualFake) ClearManualLyrics(_ context.Context, id string) (bool, error) {
	if !f.manual {
		return false, nil
	}
	f.cleared = append(f.cleared, id)
	return true, nil
}

func (f *manualFake) TrackDurationMS(_ context.Context, id string) (int64, error) {
	if id == "unknown" {
		return 0, domain.ErrNotFound
	}
	return f.duration, nil
}

func send(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func errLines(t *testing.T, rec *httptest.ResponseRecorder) []lrc.LineError {
	t.Helper()
	var body struct {
		Error apiError `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Error.Lines
}

func TestManualLyricsValidation(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name  string
		req   manualLyricsRequest
		code  string
		plain string
	}{
		{"synced derives plain", manualLyricsRequest{Synced: s("\uFEFF[00:01.00] one  \r\n[00:02.00] ♪\r\n[00:03.00] two\r\n")}, "", "one\n\ntwo"},
		{"plain kept with synced", manualLyricsRequest{Synced: s("[00:01.00] a"), Plain: s("A\r\n")}, "", "A"},
		{"plain only", manualLyricsRequest{Plain: s("  la la  \n\n")}, "", "  la la"},
		{"nothing", manualLyricsRequest{Plain: s("  \n ")}, "invalid_lyrics", ""},
		{"markers only", manualLyricsRequest{Plain: s("[Instrumental]")}, "invalid_lyrics", ""},
		{"synced markers only", manualLyricsRequest{Synced: s("[00:01.00] ♪")}, "invalid_lyrics", ""},
		{"instrumental with text", manualLyricsRequest{Instrumental: true, Plain: s("x")}, "invalid_lyrics", ""},
		{"too large", manualLyricsRequest{Plain: s(strings.Repeat("a", maxManualText+1))}, "too_large", ""},
		{"bad lrc", manualLyricsRequest{Synced: s("[00:01.00] a\nno time")}, "invalid_lrc", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l, code, _, _ := manualLyrics(c.req, 200_000)
			if code != c.code {
				t.Fatalf("code = %q, want %q", code, c.code)
			}
			if code == "" && (l.Status != domain.LyricsFound || l.Plain != c.plain) {
				t.Errorf("lyrics = %+v, want plain %q", l, c.plain)
			}
		})
	}
	l, code, _, _ := manualLyrics(manualLyricsRequest{Instrumental: true}, 0)
	if code != "" || l.Status != domain.LyricsInstrumental || l.Plain != "" || l.Synced != "" {
		t.Errorf("instrumental = %+v %s", l, code)
	}
	l, _, _, _ = manualLyrics(manualLyricsRequest{Synced: s("[00:01.00] a  \r\n[00:02.00] b")}, 0)
	if l.Synced != "[00:01.00] a\n[00:02.00] b" {
		t.Errorf("normalized synced = %q", l.Synced)
	}
}

// PUT validates against the track duration and answers 400 with per-line
// errors; a valid body is stored with the actor, audited, and nudges the
// translator when the lyrics changed.
func TestPutLyricsHandler(t *testing.T) {
	f := &fakeLyrics{manualFake: manualFake{duration: 200_000}}
	a := lyricsAPI(f)
	aud := &fakeAuditor{}
	a.auditor = aud
	nudged := 0
	a.Translation.Nudge = func() { nudged++ }
	h := ReadOnlyHandler(a)

	rec := send(h, http.MethodPut, "/tracks/t1/lyrics", `{"synced":"[00:05.00] a\n[00:01.00] b\n[00:99.00] c\n[04:00.00] d"}`)
	if rec.Code != http.StatusBadRequest || lyricsErrCode(rec) != "invalid_lrc" || len(f.set) != 0 {
		t.Fatalf("bad lrc: %d %s", rec.Code, rec.Body.String())
	}
	want := []lrc.LineError{
		{Line: 2, Message: "out of order: 0:01.00 comes before 0:05.00 on line 1"},
		{Line: 3, Message: "invalid timestamp [00:99.00]: seconds must be below 60 and minutes at most 999"},
		{Line: 4, Message: "4:00.00 is past the end of the song (3:20.00)"},
	}
	if got := errLines(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %+v", got)
	}

	for _, body := range []string{`{"lyrics":"x"}`, `not json`, `{"synced":1}`} {
		if rec := send(h, http.MethodPut, "/tracks/t1/lyrics", body); rec.Code != http.StatusBadRequest ||
			lyricsErrCode(rec) != "invalid_body" {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if rec := send(h, http.MethodPut, "/tracks/unknown/lyrics", `{"plain":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown track: %d", rec.Code)
	}

	f.retranslate = true
	rec = send(h, http.MethodPut, "/tracks/t1/lyrics", `{"synced":"[00:01.00] một\n[00:02.00] hai"}`)
	if rec.Code != http.StatusOK || len(f.set) != 1 || f.id != "t1" || !f.withSynced {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	if l := f.set[0]; l.Status != domain.LyricsFound || l.Plain != "một\nhai" || l.Synced == "" || f.setBy != "mcp" {
		t.Errorf("stored = %+v by %s", l, f.setBy)
	}
	if nudged != 1 || len(aud.calls) != 1 || aud.calls[0].action != "lyrics.manual_set" ||
		aud.calls[0].actor != "mcp" || aud.calls[0].detail["track_id"] != "t1" || aud.calls[0].detail["retranslate"] != true {
		t.Errorf("nudged %d, audit %+v", nudged, aud.calls)
	}

	// Unchanged lyrics: no audit, no nudge.
	f.unchanged, f.retranslate = true, false
	if rec := send(h, http.MethodPut, "/tracks/t1/lyrics", `{"instrumental":true}`); rec.Code != http.StatusOK ||
		nudged != 1 || len(aud.calls) != 1 || f.set[1].Status != domain.LyricsInstrumental {
		t.Errorf("unchanged: %d nudged %d audit %d", rec.Code, nudged, len(aud.calls))
	}

	if rec := send(h, http.MethodPut, "/tracks/t1/lyrics", `{"plain":"`+strings.Repeat("a", maxManualBody)+`"}`); rec.Code !=
		http.StatusRequestEntityTooLarge {
		t.Errorf("huge body: %d", rec.Code)
	}
}

// DELETE removes manual lyrics and nudges the fetcher; without manual
// lyrics it is 404.
func TestClearManualLyricsHandler(t *testing.T) {
	f := &fakeLyrics{}
	a := lyricsAPI(f)
	aud := &fakeAuditor{}
	a.auditor = aud
	nudged := 0
	a.LyricsNudge = func() { nudged++ }
	h := ReadOnlyHandler(a)
	if rec := send(h, http.MethodDelete, "/tracks/t1/lyrics/manual", ""); rec.Code != http.StatusNotFound ||
		lyricsErrCode(rec) != "no_manual_lyrics" || nudged != 0 {
		t.Errorf("no manual: %d %s", rec.Code, rec.Body.String())
	}
	f.manual = true
	if rec := send(h, http.MethodDelete, "/tracks/t1/lyrics/manual", ""); rec.Code != http.StatusOK ||
		len(f.cleared) != 1 || nudged != 1 || len(aud.calls) != 1 || aud.calls[0].action != "lyrics.manual_cleared" {
		t.Errorf("clear: %d %s audit %+v", rec.Code, rec.Body.String(), aud.calls)
	}
}

// A re-queue of manual lyrics is 409 manual_lyrics unless replace_manual.
func TestRequeueManualLyrics(t *testing.T) {
	f := &fakeLyrics{manualFake: manualFake{manual: true}}
	a := lyricsAPI(f)
	a.LyricsLookup = true
	aud := &fakeAuditor{}
	a.auditor = aud
	h := ReadOnlyHandler(a)
	for _, body := range []string{"", `{}`, `{"replace_manual":false}`} {
		if rec := send(h, http.MethodPost, "/tracks/t1/lyrics/requeue", body); rec.Code != http.StatusConflict ||
			lyricsErrCode(rec) != "manual_lyrics" || len(f.lyricsReq) != 0 {
			t.Errorf("%q: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if rec := send(h, http.MethodPost, "/tracks/t1/lyrics/requeue", `{"replace":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", rec.Code)
	}
	rec := send(h, http.MethodPost, "/tracks/t1/lyrics/requeue", `{"replace_manual":true}`)
	if rec.Code != http.StatusAccepted || len(f.lyricsReq) != 1 || !f.replaced {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}
	if len(aud.calls) != 1 || aud.calls[0].detail["replace_manual"] != true {
		t.Errorf("audit = %+v", aud.calls)
	}
}
