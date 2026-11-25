package lrclib

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

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
	// Plain lyrics do not flip it, nor marker-only synced text.
	s = &server{get: jsonBody(inst), search: jsonBody(`[{"id":9,"trackName":"Intro","artistName":"A","duration":60,"plainLyrics":"words"},
		{"id":10,"trackName":"Intro","artistName":"A","duration":60,"syncedLyrics":"[00:01.00] ♪
[00:05.00] [Instrumental]"}]`)}
	if l := lookup(t, s, c); l == nil || l.Status != domain.LyricsInstrumental || l.LRCLibID != 5 {
		t.Fatalf("lyrics = %+v, want the instrumental exact match", l)
	}
	// A matching entry with synced lyrics wins over it.
	s = &server{get: jsonBody(inst), search: jsonBody(`[{"id":11,"trackName":"Intro","artistName":"A","duration":60,"syncedLyrics":"[00:01.00] words"}]`)}
	if l := lookup(t, s, c); l == nil || l.Status != domain.LyricsFound || l.LRCLibID != 11 {
		t.Fatalf("lyrics = %+v, want the synced search entry", l)
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

// getBy answers /api/get with the handler for its artist_name, else 404.
func getBy(hs map[string]http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h, ok := hs[r.URL.Query().Get("artist_name")]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func hang(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }

// An HTTP client timeout keeps the exact match; a cancelled ctx does not.
func TestLookupKeepsExactMatchOnClientTimeout(t *testing.T) {
	s := &server{get: byArtist(map[string]string{"VƯƠNG BÌNH": duoPlain}), search: hang}
	c := s.start(t)
	c.http.Timeout = 200 * time.Millisecond
	l, err := c.Lookup(context.Background(), duo)
	if err != nil || l == nil || l.LRCLibID != 40466323 {
		t.Fatalf("lookup = %+v, %v; want the exact plain entry", l, err)
	}
	s = &server{get: byArtist(map[string]string{"VƯƠNG BÌNH": duoPlain}), search: hang}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if l, err := s.start(t).Lookup(ctx, duo); err == nil {
		t.Fatalf("cancelled lookup = %+v, want an error", l)
	}
}

func TestLookupGetErrorsContinue(t *testing.T) {
	fail := status(http.StatusInternalServerError, nil)
	// A failing all-artists Get goes on to the first-artist Get.
	s := &server{get: getBy(map[string]http.HandlerFunc{"VƯƠNG BÌNH, Lâm Bảo Ngọc": fail, "VƯƠNG BÌNH": jsonBody(duoSynced)})}
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 40346691 {
		t.Fatalf("lyrics = %+v", l)
	}
	wantCalls(t, s, getAll, getFirst)
	// Nothing found after a failed Get: the error is returned (retry, not not_found).
	s = &server{get: fail, search: jsonBody(`[]`)}
	if l, err := s.start(t).Lookup(context.Background(), duo); err == nil {
		t.Fatalf("lookup = %+v, want the Get error", l)
	}
	// A plain all-artists match survives a failing first-artist Get.
	allPlain := strings.Replace(duoPlain, `"artistName":"VƯƠNG BÌNH"`, `"artistName":"VƯƠNG BÌNH, Lâm Bảo Ngọc"`, 1)
	s = &server{get: getBy(map[string]http.HandlerFunc{"VƯƠNG BÌNH, Lâm Bảo Ngọc": jsonBody(allPlain), "VƯƠNG BÌNH": fail}),
		search: jsonBody(`[]`)}
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 40466323 || l.Status != domain.LyricsFound {
		t.Fatalf("lyrics = %+v, want the plain all-artists entry", l)
	}
	wantCalls(t, s, getAll, getFirst, search)
	// The search may still upgrade it to synced.
	s = &server{get: getBy(map[string]http.HandlerFunc{"VƯƠNG BÌNH, Lâm Bảo Ngọc": jsonBody(allPlain), "VƯƠNG BÌNH": fail}),
		search: jsonBody(`[` + duoSynced + `]`)}
	if l := lookup(t, s, duo); l == nil || l.LRCLibID != 40346691 {
		t.Fatalf("lyrics = %+v, want the synced entry", l)
	}
}

// An instrumental all-artists Get gives way to a plain first-artist Get.
func TestLookupGetUpgradesInstrumentalToPlain(t *testing.T) {
	s := &server{get: byArtist(map[string]string{
		"VƯƠNG BÌNH, Lâm Bảo Ngọc": `{"id":1,"trackName":"x","artistName":"VƯƠNG BÌNH, Lâm Bảo Ngọc","duration":308,"instrumental":true}`,
		"VƯƠNG BÌNH":               duoPlain,
	}), search: jsonBody(`[]`)}
	if l := lookup(t, s, duo); l == nil || l.Status != domain.LyricsFound || l.LRCLibID != 40466323 {
		t.Fatalf("lyrics = %+v, want the plain entry", l)
	}
}

func TestCredited(t *testing.T) {
	for _, tc := range []struct {
		c    domain.LyricsCandidate
		want string
	}{
		{domain.LyricsCandidate{Artist: "A", Artists: []string{" A ", "", "  ", "B"}}, "A|B"},
		{domain.LyricsCandidate{Artist: " Solo "}, "Solo"},
		{domain.LyricsCandidate{Artist: "", Artists: []string{}}, ""},
		{domain.LyricsCandidate{Artists: []string{" "}}, ""},
	} {
		if got := strings.Join(credited(tc.c), "|"); got != tc.want {
			t.Errorf("credited(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
	// No artist at all: no request.
	s := &server{}
	if l := lookup(t, s, domain.LyricsCandidate{Name: "x", Album: "y", DurationMS: 1000}); l != nil || len(s.reqs) != 0 {
		t.Errorf("lyrics = %+v, requests = %d", l, len(s.reqs))
	}
}

func TestArtistNamesAndShortNames(t *testing.T) {
	got := strings.Join(artistNames("Đen feat. MIN & Hoàng Dũng x GDucky, Lâm Bảo Ngọc (ft. Xám)"), "|")
	if got != "den|min|hoang dung|gducky|lam bao ngoc|xam" {
		t.Errorf("names = %q", got)
	}
	for _, tc := range []struct {
		credit, cand string
		ok           bool
	}{
		{"V", "V", true},
		{"V", "V, Y", true},
		{"V", "Vũ Cát Tường", false},
		{"Y", "Mỹ Tâm", false},
		{"Ni", "Dennis", false},
		{"Ni", "Nini", false},
		{"Ni", "Ni & Tùng", true},
		{"Sơn Tùng", "Sơn Tùng M-TP", true},
		{"GDucky", "Hoàng Dũng x GDucky", true},
		{"Đen", "Đen feat. MIN", true},
		{"Đen", "Hà Nhi", false},
		{"Malcolm X", "Malcolm X", true},
	} {
		if got := artistMatch(artistNames(tc.cand), artistNames(tc.credit)); got != tc.ok {
			t.Errorf("%q vs %q = %v, want %v", tc.credit, tc.cand, got, tc.ok)
		}
	}
	// Short names in the search: only whole names match.
	recs := []Record{
		{ID: 1, TrackName: "Song", ArtistName: "Vũ", Duration: 200, SyncedLyrics: ptr("[00:01.00] a")},
		{ID: 2, TrackName: "Song", ArtistName: "Dennis", Duration: 200, SyncedLyrics: ptr("[00:01.00] a")},
	}
	if r := pick(recs, "Song", []string{"V", "Ni"}, 200, nil); r != nil {
		t.Errorf("short names picked %+v", r)
	}
	recs = append(recs, Record{ID: 3, TrackName: "Song", ArtistName: "Ni, V", Duration: 200, PlainLyrics: ptr("a")})
	if r := pick(recs, "Song", []string{"V", "Ni"}, 200, nil); r == nil || r.ID != 3 {
		t.Errorf("whole short names = %+v", r)
	}
}

// With an exact match in hand, a replacement must credit the first artist:
// not a cover by someone else, not the featured artist alone.
func TestLookupReplacementNeedsFirstArtist(t *testing.T) {
	entry := func(artist string) string {
		return `{"id":7,"trackName":"` + duoName + `","artistName":"` + artist +
			`","duration":308,"syncedLyrics":"[00:01.00] lời"}`
	}
	for _, tc := range []struct {
		artist string
		want   int64
	}{
		{"Hà Nhi", 40466323},       // a cover
		{"Lâm Bảo Ngọc", 40466323}, // the featured artist alone
		{"Bình", 40466323},         // a part of the name
		{"VƯƠNG BÌNH ft. Lâm Bảo Ngọc", 7},
		{"Lâm Bảo Ngọc & Vương Bình", 7},
	} {
		s := &server{get: byArtist(map[string]string{"VƯƠNG BÌNH": duoPlain}), search: jsonBody(`[` + entry(tc.artist) + `]`)}
		if l := lookup(t, s, duo); l == nil || l.LRCLibID != tc.want {
			t.Errorf("%q: lyrics = %+v, want id %d", tc.artist, l, tc.want)
		}
	}
	// Without an exact match the featured artist's entry is fine.
	s := &server{search: jsonBody(`[` + entry("Lâm Bảo Ngọc") + `]`)}
	if l := lookup(t, s, domain.LyricsCandidate{Name: duoName, Artists: []string{"VƯƠNG BÌNH", "Lâm Bảo Ngọc"}}); l == nil || l.LRCLibID != 7 {
		t.Errorf("no exact match: %+v", l)
	}
}

func TestMarkerOnlyIsInstrumental(t *testing.T) {
	for _, tc := range []struct {
		plain, synced *string
		status, kept  string
	}{
		{ptr("[Instrumental]"), nil, domain.LyricsInstrumental, ""},
		{ptr(" instrumental \n"), nil, domain.LyricsInstrumental, ""},
		{ptr("♪ ♪\n♫"), nil, domain.LyricsInstrumental, ""},
		{ptr("(Nhạc dạo)"), nil, domain.LyricsFound, ""},
		{nil, ptr("[00:01.00] ♪\n[00:09.50] [Instrumental]"), domain.LyricsInstrumental, ""},
		{ptr("words"), ptr("[00:01.00] ♪"), domain.LyricsFound, ""},
		{ptr("[Instrumental]"), ptr("[00:01.00] words"), domain.LyricsFound, "[00:01.00] words"},
	} {
		l := toLyrics(&Record{ID: 1, PlainLyrics: tc.plain, SyncedLyrics: tc.synced})
		if l.Status != tc.status || l.Synced != tc.kept {
			t.Errorf("%v/%v = %+v", tc.plain, tc.synced, l)
		}
	}
	if k := lyricsKind(&Record{ID: 1, SyncedLyrics: ptr("[00:01.00] ♪")}); k != kindNone {
		t.Errorf("marker-only synced kind = %d", k)
	}
}
