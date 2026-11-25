package lrclib

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

func ptr(s string) *string { return &s }

// server answers /api/get and /api/search with the given handlers and
// records the requests.
type server struct {
	get, search http.HandlerFunc
	reqs        []*http.Request
}

func (s *server) start(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.reqs = append(s.reqs, r)
		switch r.URL.Path {
		case "/api/get":
			if s.get != nil {
				s.get(w, r)
				return
			}
		case "/api/search":
			if s.search != nil {
				s.search(w, r)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", 0, srv.Client())
}

func jsonBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func status(code int, h http.Header) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range h {
			w.Header()[k] = v
		}
		w.WriteHeader(code)
	}
}

var cand = domain.LyricsCandidate{TrackID: "t1", Name: "Như Anh Mơ", Artist: "PC", Album: "Như Anh Mơ", DurationMS: 358_413}

func TestGetRequest(t *testing.T) {
	s := &server{get: jsonBody(`{"id":7,"trackName":"Như Anh Mơ","duration":358,"plainLyrics":"a\nb","syncedLyrics":"[00:01.00] a\n[00:02.00] b"}`)}
	c := s.start(t)
	l, err := c.Lookup(context.Background(), cand)
	if err != nil {
		t.Fatal(err)
	}
	if l == nil || l.Status != domain.LyricsFound || l.LRCLibID != 7 || l.Plain != "a\nb" || !strings.HasPrefix(l.Synced, "[00:01.00]") {
		t.Fatalf("lyrics = %+v", l)
	}
	r := s.reqs[0]
	q := r.URL.Query()
	if q.Get("track_name") != "Như Anh Mơ" || q.Get("artist_name") != "PC" || q.Get("album_name") != "Như Anh Mơ" ||
		q.Get("duration") != "358" {
		t.Errorf("query = %v", q)
	}
	if r.Header.Get("User-Agent") != UserAgent || r.Header.Get("Accept") != "application/json" {
		t.Errorf("headers = %v", r.Header)
	}
	if len(s.reqs) != 1 {
		t.Errorf("requests = %d, want 1 (no search after a hit)", len(s.reqs))
	}
}

func TestStatuses(t *testing.T) {
	ctx := context.Background()
	c := (&server{get: status(http.StatusNotFound, nil)}).start(t)
	if rec, err := c.Get(ctx, "a", "b", "c", 1); rec != nil || err != nil {
		t.Errorf("404 = %v, %v; want nil, nil", rec, err)
	}

	c = (&server{get: status(http.StatusTooManyRequests, http.Header{"Retry-After": {"7"}})}).start(t)
	var rl *RateLimitError
	if _, err := c.Get(ctx, "a", "b", "c", 1); !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
		t.Errorf("429 = %v", err)
	}
	c = (&server{get: status(http.StatusTooManyRequests, nil)}).start(t)
	if _, err := c.Get(ctx, "a", "b", "c", 1); !errors.As(err, &rl) || rl.RetryAfter != time.Minute {
		t.Errorf("429 without Retry-After = %v", err)
	}
	c = (&server{get: status(http.StatusTooManyRequests, http.Header{"Retry-After": {"99999"}})}).start(t)
	if _, err := c.Get(ctx, "a", "b", "c", 1); !errors.As(err, &rl) || rl.RetryAfter != time.Hour {
		t.Errorf("429 with a huge Retry-After = %v", err)
	}

	var se *StatusError
	c = (&server{get: status(http.StatusInternalServerError, nil)}).start(t)
	if _, err := c.Get(ctx, "a", "b", "c", 1); !errors.As(err, &se) || se.Permanent() {
		t.Errorf("500 = %v, want transient StatusError", err)
	}
	c = (&server{get: status(http.StatusBadRequest, nil)}).start(t)
	if _, err := c.Get(ctx, "a", "b", "c", 1); !errors.As(err, &se) || !se.Permanent() {
		t.Errorf("400 = %v, want permanent StatusError", err)
	}
	if (&StatusError{Code: http.StatusRequestTimeout}).Permanent() {
		t.Error("408 must not be permanent")
	}
}

func TestOversizedBody(t *testing.T) {
	big := `{"id":1,"plainLyrics":"` + strings.Repeat("x", maxBody+10) + `"}`
	c := (&server{get: jsonBody(big)}).start(t)
	if _, err := c.Get(context.Background(), "a", "b", "c", 1); err == nil {
		t.Fatal("expected an error for a body over the cap")
	}
}

func TestLookupFallsBackToSearch(t *testing.T) {
	s := &server{
		get: status(http.StatusNotFound, nil),
		search: jsonBody(`[
			{"id":1,"trackName":"Other","artistName":"PC","duration":358,"plainLyrics":"other"},
			{"id":2,"trackName":"nhu anh mo","artistName":"PC","duration":359.5,"plainLyrics":"plain only"},
			{"id":3,"trackName":"Như Anh Mơ","artistName":"PC, Đạt G","duration":357,"plainLyrics":"p","syncedLyrics":"[00:01.00] p"},
			{"id":4,"trackName":"Như Anh Mơ","artistName":"PC","duration":300,"plainLyrics":"too short"},
			{"id":5,"trackName":"Như Anh Mơ","artistName":"Someone Else","duration":358,"plainLyrics":"x","syncedLyrics":"[00:01.00] x"}]`),
	}
	l, err := s.start(t).Lookup(context.Background(), cand)
	if err != nil {
		t.Fatal(err)
	}
	if l == nil || l.LRCLibID != 3 {
		t.Fatalf("picked %+v, want id 3 (name match with synced lyrics)", l)
	}
	if len(s.reqs) != 2 || s.reqs[1].URL.Path != "/api/search" || s.reqs[1].URL.Query().Get("artist_name") != "PC" {
		t.Errorf("requests = %d", len(s.reqs))
	}
}

func TestLookupRejectsFarDuration(t *testing.T) {
	s := &server{get: status(http.StatusNotFound, nil),
		search: jsonBody(`[{"id":1,"trackName":"Như Anh Mơ","artistName":"PC","duration":361.5,"plainLyrics":"x"}]`)}
	l, err := s.start(t).Lookup(context.Background(), cand)
	if err != nil || l != nil {
		t.Fatalf("lookup = %+v, %v; want nil, nil (3 s off)", l, err)
	}
}

func TestLookupSkipsGetWithoutAlbumOrDuration(t *testing.T) {
	for _, c := range []domain.LyricsCandidate{
		{Name: "Song", Artist: "A", DurationMS: 200_000},
		{Name: "Song", Artist: "A", Album: "X"},
	} {
		s := &server{get: jsonBody(`{"id":9,"plainLyrics":"x"}`),
			search: jsonBody(`[{"id":5,"trackName":"song","artistName":"a","duration":200,"plainLyrics":"x"}]`)}
		l, err := s.start(t).Lookup(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range s.reqs {
			if r.URL.Path == "/api/get" {
				t.Errorf("%+v: called /api/get", c)
			}
		}
		if l == nil || l.LRCLibID != 5 {
			t.Errorf("%+v: lyrics = %+v", c, l)
		}
	}
	// Without a duration, only an exact (folded) name match counts.
	s := &server{search: jsonBody(`[{"id":5,"trackName":"Songs","artistName":"A","duration":200,"plainLyrics":"x"}]`)}
	if l, _ := s.start(t).Lookup(context.Background(), domain.LyricsCandidate{Name: "Song", Artist: "A"}); l != nil {
		t.Errorf("name mismatch without duration picked %+v", l)
	}
}

func TestToLyrics(t *testing.T) {
	if l := toLyrics(&Record{ID: 1, Instrumental: true, PlainLyrics: ptr("x")}); l.Status != domain.LyricsInstrumental || l.Plain != "" {
		t.Errorf("instrumental flag = %+v", l)
	}
	if l := toLyrics(&Record{ID: 1, PlainLyrics: ptr("  "), SyncedLyrics: ptr("")}); l.Status != domain.LyricsInstrumental {
		t.Errorf("blank texts = %+v", l)
	}
	l := toLyrics(&Record{ID: 1, SyncedLyrics: ptr("[00:12.34] Line one\r\n[01:02.5]Line two\r[03:04] Line three")})
	if l.Status != domain.LyricsFound || l.Plain != "Line one\nLine two\nLine three" ||
		l.Synced != "[00:12.34] Line one\n[01:02.5]Line two\n[03:04] Line three" {
		t.Errorf("derived plain = %q, synced = %q", l.Plain, l.Synced)
	}
	// Synced text with timestamps only leaves no plain text: instrumental.
	if l := toLyrics(&Record{ID: 1, SyncedLyrics: ptr("[00:01.00]\n[00:02.00] ")}); l.Status != domain.LyricsInstrumental || l.Plain != "" {
		t.Errorf("timestamps only = %+v", l)
	}
	// NUL bytes are removed and line breaks normalized.
	if l := toLyrics(&Record{ID: 1, PlainLyrics: ptr("a\x00b\r\nc\rd")}); l.Plain != "ab\nc\nd" {
		t.Errorf("cleaned plain = %q", l.Plain)
	}
	if l := toLyrics(&Record{ID: 1, PlainLyrics: ptr("\x00\x00")}); l.Status != domain.LyricsInstrumental {
		t.Errorf("NUL-only = %+v", l)
	}
}

func TestCapText(t *testing.T) {
	line := strings.Repeat("a", 999) + "\n"
	long := strings.Repeat(line, 300) // 300 000 bytes
	got := toLyrics(&Record{ID: 1, PlainLyrics: &long})
	if len(got.Plain) > maxText || !strings.HasSuffix(got.Plain, "a") || len(got.Plain)%1000 != 999 {
		t.Errorf("capped plain length = %d", len(got.Plain))
	}
	// No newline: the cut stays on a rune boundary.
	s := strings.Repeat("ơ", maxText) // 2 bytes each
	if c := capText(s); len(c) > maxText || !strings.HasPrefix(s, c) || len(c)%2 != 0 {
		t.Errorf("rune cut length = %d", len(c))
	}
}

func TestFold(t *testing.T) {
	if fold(" Như Anh Mơ ") != "nhu anh mo" || fold("Đạt G") != "dat g" {
		t.Errorf("fold = %q, %q", fold(" Như Anh Mơ "), fold("Đạt G"))
	}
}

func TestRateLimiterSpacesRequests(t *testing.T) {
	s := &server{get: status(http.StatusNotFound, nil)}
	c := s.start(t)
	c2 := New(c.base, 50*time.Millisecond, c.http)
	start := time.Now()
	for range 3 {
		if _, err := c2.Get(context.Background(), "a", "b", "c", 1); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 90*time.Millisecond {
		t.Errorf("3 requests took %s, want >= 100ms with 50ms spacing", d)
	}
}

func TestEmptyRecordIsNotFound(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"id":0,"plainLyrics":"x"}`} {
		s := &server{get: jsonBody(body), search: jsonBody(`[]`)}
		l, err := s.start(t).Lookup(context.Background(), cand)
		if err != nil || l != nil {
			t.Errorf("%s = %+v, %v; want nil, nil", body, l, err)
		}
		if len(s.reqs) != 2 {
			t.Errorf("%s: requests = %d, want get then search", body, len(s.reqs))
		}
	}
	// Search candidates without an id are skipped too.
	if r := pick([]Record{{TrackName: "Song", ArtistName: "A", PlainLyrics: ptr("x")}}, "Song", []string{"A"}, 0, nil); r != nil {
		t.Errorf("picked a record without id: %+v", r)
	}
}

func TestPickNameAndArtist(t *testing.T) {
	rec := func(id int64, name, artist string) Record {
		return Record{ID: id, TrackName: name, ArtistName: artist, Duration: 200, PlainLyrics: ptr("x")}
	}
	for _, tc := range []struct {
		name, artist string
		r            Record
		ok           bool
	}{
		{"Sẽ Là", "Dick", rec(1, "se la", "DICK"), true},
		{"Sẽ Là", "Dick", rec(1, "Sẽ Là (Live)", "Dick"), true},
		{"Sẽ Là - Remastered 2011", "Dick", rec(1, "Sẽ Là", "Dick"), true},
		{"Sẽ Là", "Dick", rec(1, "Sẽ Là [Remix] - Radio Edit", "Dick, Xám"), true},
		{"Sẽ Là", "Dick ft. Xám", rec(1, "Sẽ Là", "Dick"), true},
		{"Sẽ Là", "Dick", rec(1, "Sẽ Là Nữa", "Dick"), false},
		{"Sẽ Là", "Dick", rec(1, "Sẽ Là", "Other"), false},
		{"Sẽ Là", "Dick", rec(1, "Sẽ Là", ""), false},
		{"(Intro)", "Dick", rec(1, "(intro)", "Dick"), true},
	} {
		got := pick([]Record{tc.r}, tc.name, []string{tc.artist}, 200, nil)
		if (got != nil) != tc.ok {
			t.Errorf("%q/%q vs %q/%q: picked %v, want %v", tc.name, tc.artist, tc.r.TrackName, tc.r.ArtistName, got != nil, tc.ok)
		}
	}
	// The exact folded name wins over a suffixed one.
	got := pick([]Record{rec(1, "Sẽ Là (Live)", "Dick"), rec(2, "Sẽ Là", "Dick")}, "Sẽ Là", []string{"Dick"}, 200, nil)
	if got == nil || got.ID != 2 {
		t.Errorf("exact name = %+v", got)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	at := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	if d := retryAfter(at); d < 100*time.Second || d > 2*time.Minute {
		t.Errorf("HTTP date = %s, want about 2m", d)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	for _, v := range []string{past, "soon", "", "-5"} {
		if d := retryAfter(v); d != time.Minute {
			t.Errorf("%q = %s, want the 1m default", v, d)
		}
	}
	if d := retryAfter(time.Now().Add(5 * time.Hour).UTC().Format(http.TimeFormat)); d != time.Hour {
		t.Errorf("far date = %s, want the 1h cap", d)
	}
}

// The LRCLIB example: one song, two entries. 40466323 credits the first
// artist and has plain lyrics only; 40346691 credits both and is synced.
const (
	duoName  = "Không biết phải nói gì không biết phải làm sao"
	duoAlbum = "ANH BỜ VAI (Ấn Bản KIM)"
	duoPlain = `{"id":40466323,"trackName":"Không biết phải nói gì không biết phải làm sao","artistName":"VƯƠNG BÌNH",
		"duration":308,"plainLyrics":"lời"}`
	duoSynced = `{"id":40346691,"trackName":"Không biết phải nói gì không biết phải làm sao",
		"artistName":"VƯƠNG BÌNH, Lâm Bảo Ngọc","duration":308,"plainLyrics":"lời","syncedLyrics":"[00:01.00] lời"}`
)

var duo = domain.LyricsCandidate{TrackID: "t2", Name: duoName, Artist: "VƯƠNG BÌNH",
	Artists: []string{"VƯƠNG BÌNH", "Lâm Bảo Ngọc"}, Album: duoAlbum, DurationMS: 308_200}

// byArtist answers /api/get with the body for its artist_name, else 404.
func byArtist(bodies map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if b, ok := bodies[r.URL.Query().Get("artist_name")]; ok {
			jsonBody(b)(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

// calls lists the requests as "path artist_name".
func (s *server) calls() []string {
	out := make([]string, len(s.reqs))
	for i, r := range s.reqs {
		out[i] = r.URL.Path + " " + r.URL.Query().Get("artist_name")
	}
	return out
}

func lookup(t *testing.T, s *server, c domain.LyricsCandidate) *domain.Lyrics {
	t.Helper()
	l, err := s.start(t).Lookup(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func wantCalls(t *testing.T, s *server, want ...string) {
	t.Helper()
	if got := s.calls(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %q, want %q", got, want)
	}
}

const (
	getAll   = "/api/get VƯƠNG BÌNH, Lâm Bảo Ngọc"
	getFirst = "/api/get VƯƠNG BÌNH"
	search   = "/api/search VƯƠNG BÌNH"
)

func TestLookupAllArtistsGetWins(t *testing.T) {
	s := &server{get: byArtist(map[string]string{"VƯƠNG BÌNH, Lâm Bảo Ngọc": duoSynced, "VƯƠNG BÌNH": duoPlain})}
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 40346691 || l.Synced == "" {
		t.Fatalf("lyrics = %+v, want the synced entry", l)
	}
	wantCalls(t, s, getAll) // synced at once: one request
	if q := s.reqs[0].URL.Query(); q.Get("album_name") != duoAlbum || q.Get("duration") != "308" {
		t.Errorf("query = %v", q)
	}
}

func TestLookupPlainGetThenSearchFindsSynced(t *testing.T) {
	s := &server{get: byArtist(map[string]string{"VƯƠNG BÌNH": duoPlain}),
		search: jsonBody(`[` + duoPlain + `,` + duoSynced + `]`)}
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 40346691 || l.Synced == "" {
		t.Fatalf("lyrics = %+v, want the synced search entry", l)
	}
	wantCalls(t, s, getAll, getFirst, search) // the worst case: three requests
}

func TestLookupSingleArtistSkipsSecondGet(t *testing.T) {
	s := &server{get: jsonBody(`{"id":7,"trackName":"Như Anh Mơ","artistName":"PC","duration":358,"plainLyrics":"a"}`),
		search: jsonBody(`[]`)}
	if l := lookup(t, s, domain.LyricsCandidate{Name: "Như Anh Mơ", Artist: "PC", Artists: []string{"PC"},
		Album: "Như Anh Mơ", DurationMS: 358_413}); l == nil || l.LRCLibID != 7 || l.Plain != "a" {
		t.Fatalf("lyrics = %+v, want the plain exact match", l)
	}
	wantCalls(t, s, "/api/get PC", "/api/search PC")
}

func TestLookupExactPlainBeatsSearchPlainAndFarSynced(t *testing.T) {
	s := &server{get: byArtist(map[string]string{"VƯƠNG BÌNH": duoPlain}), search: jsonBody(`[
		{"id":1,"trackName":"` + duoName + `","artistName":"VƯƠNG BÌNH","duration":308,"plainLyrics":"other plain"},
		{"id":2,"trackName":"` + duoName + `","artistName":"VƯƠNG BÌNH","duration":310.5,"syncedLyrics":"[00:01.00] far"},
		{"id":3,"trackName":"` + duoName + ` (Live)","artistName":"VƯƠNG BÌNH","duration":308,"syncedLyrics":"[00:01.00] live"},
		{"id":4,"trackName":"` + duoName + `","artistName":"Someone Else","duration":308,"syncedLyrics":"[00:01.00] x"}]`)}
	// 2.5 s off, a version suffix and another artist do not replace the
	// exact match; nor does another plain entry.
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 40466323 {
		t.Fatalf("lyrics = %+v, want the exact plain entry", l)
	}
}

func TestLookupFoldsDiacritics(t *testing.T) {
	s := &server{get: byArtist(map[string]string{"VƯƠNG BÌNH": duoPlain}), search: jsonBody(`[
		{"id":9,"trackName":"KHONG BIET PHAI NOI GI KHONG BIET PHAI LAM SAO","artistName":"vuong binh, lam bao ngoc",
		"duration":307,"syncedLyrics":"[00:01.00] lời"}]`)}
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 9 {
		t.Fatalf("lyrics = %+v, want the folded synced match", l)
	}
	// Any credited artist matches, not only the first.
	s = &server{search: jsonBody(`[{"id":9,"trackName":"Song","artistName":"lam bao ngoc","duration":200,"plainLyrics":"x"}]`)}
	if l := lookup(t, s, domain.LyricsCandidate{Name: "Song", Artist: "VƯƠNG BÌNH", Artists: []string{"VƯƠNG BÌNH", "Lâm Bảo Ngọc"},
		DurationMS: 200_000}); l == nil || l.LRCLibID != 9 {
		t.Fatalf("second artist = %+v", l)
	}
}

func TestLookupInstrumentalStaysInstrumental(t *testing.T) {
	inst := `{"id":5,"trackName":"Intro","artistName":"A","duration":60,"instrumental":true}`
	c := domain.LyricsCandidate{Name: "Intro", Artist: "A", Artists: []string{"A"}, Album: "X", DurationMS: 60_000}
	s := &server{get: jsonBody(inst), search: jsonBody(`[` + inst + `,
		{"id":6,"trackName":"Intro","artistName":"A","duration":60,"instrumental":true,"plainLyrics":"flagged"},
		{"id":7,"trackName":"Intro","artistName":"B","duration":60,"plainLyrics":"other artist"},
		{"id":8,"trackName":"Intro","artistName":"A","duration":64,"plainLyrics":"too long"}]`)}
	if l := lookup(t, s, c); l == nil || l.Status != domain.LyricsInstrumental || l.LRCLibID != 5 {
		t.Fatalf("lyrics = %+v, want the instrumental exact match", l)
	}
	// A matching entry that really has lyrics wins over it.
	s = &server{get: jsonBody(inst), search: jsonBody(`[{"id":9,"trackName":"Intro","artistName":"A","duration":60,"plainLyrics":"words"}]`)}
	if l := lookup(t, s, c); l == nil || l.Status != domain.LyricsFound || l.LRCLibID != 9 {
		t.Fatalf("lyrics = %+v, want the plain search entry", l)
	}
}

func TestLookupNotFoundThenSearch(t *testing.T) {
	s := &server{get: status(http.StatusNotFound, nil), search: jsonBody(`[` + duoSynced + `]`)}
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 40346691 {
		t.Fatalf("lyrics = %+v", l)
	}
	wantCalls(t, s, getAll, getFirst, search)
}

func TestLookupKeepsExactMatchWhenSearchFails(t *testing.T) {
	s := &server{get: byArtist(map[string]string{"VƯƠNG BÌNH": duoPlain}), search: status(http.StatusBadGateway, nil)}
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 40466323 {
		t.Fatalf("lyrics = %+v, want the exact plain entry", l)
	}
	// A rate limit is returned, so the whole lookup is retried.
	s = &server{get: byArtist(map[string]string{"VƯƠNG BÌNH": duoPlain}),
		search: status(http.StatusTooManyRequests, http.Header{"Retry-After": {"5"}})}
	var rl *RateLimitError
	if _, err := s.start(t).Lookup(context.Background(), duo); !errors.As(err, &rl) {
		t.Fatalf("err = %v, want a rate limit", err)
	}
}
