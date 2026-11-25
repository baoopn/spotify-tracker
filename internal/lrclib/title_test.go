package lrclib

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

func TestNormName(t *testing.T) {
	for in, want := range map[string]string{
		"  Như  Anh   Mơ ": "nhu anh mo",
		"ＨＥＬＬＯ　Ｗｏｒｌｄ":      "hello world",
		"Don’t Stop":       "dont stop",
		"Don't Stop":       "dont stop",
		"“Quoted”, Title!": "quoted title",
		"A・B·C":            "a b c",
		"假如风有颜色（电视剧插曲）":    "假如风有颜色 电视剧插曲",
		"???": "???",
		"":    "",
	} {
		if got := normName(in); got != want {
			t.Errorf("normName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleVariants(t *testing.T) {
	for in, want := range map[string]string{
		"假如风有颜色(电视剧《白色橄榄树》插曲)": "假如风有颜色(电视剧《白色橄榄树》插曲)|假如风有颜色",
		"假如风有颜色（电视剧插曲）":        "假如风有颜色（电视剧插曲）|假如风有颜色",
		"Song (feat. X)":                "Song (feat. X)|Song",
		"Song feat. X":                  "Song feat. X|Song",
		"Song (Live) - Remastered 2011": "Song (Live) - Remastered 2011|Song",
		"Song - From \"Movie\"":         "Song - From \"Movie\"|Song",
		"【MV】Song (Live)":               "【MV】Song (Live)|【MV】Song|Song",
		"残酷な天使のテーゼ「新世紀エヴァンゲリオン」": "残酷な天使のテーゼ「新世紀エヴァンゲリオン」|残酷な天使のテーゼ",
		"『ありがとう』":           "『ありがとう』|ありがとう",
		"君の名は ~前前前世~":       "君の名は ~前前前世~|君の名は",
		"(Intro)":           "(Intro)|Intro", // stripping never empties a name
		"【MV】":              "【MV】|MV",
		"Ｓｏｎｇ":              "Ｓｏｎｇ|Song",
		"Song (Live":        "Song (Live",
		"Plain":             "Plain",
		"Don’t Stop (Live)": "Don’t Stop (Live)|Don't Stop",
		"  ":                "",
	} {
		if got := strings.Join(titleVariants(in), "|"); got != want {
			t.Errorf("titleVariants(%q) = %q, want %q", in, got, want)
		}
	}
	for _, v := range []string{"(Intro)", "【MV】", "()", "[ ]", "- Live"} {
		for _, got := range titleVariants(v) {
			if strings.TrimSpace(got) == "" {
				t.Errorf("titleVariants(%q) has a blank variant", v)
			}
		}
		if parseTitle(v).core == "" {
			t.Errorf("core of %q is empty", v)
		}
	}
}

func TestVersionTags(t *testing.T) {
	for in, want := range map[string]string{
		"Song":                    "",
		"Song (Live)":             "live",
		"Song - Live at Wembley":  "live",
		"Live Forever":            "", // the title itself, not a tag
		"Song (Acoustic) [Remix]": "acoustic|remix",
		"假如风有颜色 (DJ版)":            "remix",
		"假如风有颜色 (伴奏)":             "inst",
		"假如风有颜色 (Live版)":          "live",
		"假如风有颜色(现场)":              "live",
		"Song (Instrumental)":     "inst",
		"Song (Karaoke Version)":  "inst",
		"Song - Remastered 2011":  "",
		"Song (feat. Live Band)":  "", // a featured artist
		"Song (with DJ Snake)":    "",
		"Song (Sped Up)":          "speed",
		"Song (粤语版)":              "lang-yue",
		"Song (From \"Movie\")":   "",
		"假如风有颜色(电视剧《白色橄榄树》插曲)": "",
	} {
		if got := parseTitle(in).tags; got != want {
			t.Errorf("tags(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleMatch(t *testing.T) {
	for _, tc := range []struct {
		track, rec string
		want       int
	}{
		{"假如风有颜色(电视剧《白色橄榄树》插曲)", "假如风有颜色", 1},
		{"假如风有颜色(电视剧《白色橄榄树》插曲)", "假如风有颜色（电视剧《白色橄榄树》插曲）", 2},
		{"Song (feat. X)", "Song", 1},
		{"Song", "Song (feat. X)", 1},
		{"Song - Remastered 2011", "Song", 1},
		{"Song (From \"Movie\")", "Song", 1},
		{"残酷な天使のテーゼ「新世紀エヴァンゲリオン」", "残酷な天使のテーゼ", 1},
		{"『ありがとう』", "ありがとう", 2},
		{"ありがとう", "『ありがとう』", 2},
		{"ＨＥＬＬＯ", "hello", 2},
		{"Don’t Stop", "Dont Stop", 2},
		{"Song (Live)", "Song", 0},
		{"Song", "Song (Live)", 0},
		{"Song (Live)", "Song - Live", 2},
		{"Song (Live)", "Song (Live at Wembley)", 1},
		{"Song (Live) - Remastered", "Song [Live]", 1},
		{"假如风有颜色 (伴奏)", "假如风有颜色", 0},
		{"假如风有颜色 (DJ版)", "假如风有颜色", 0},
		{"Song (Remix)", "Song (Acoustic)", 0},
		{"Song", "Songs", 0},
		{"(Intro)", "Intro", 2},
		{"", "", 0},
	} {
		if got := parseTitle(tc.track).match(tc.rec); got != tc.want {
			t.Errorf("match(%q, %q) = %d, want %d", tc.track, tc.rec, got, tc.want)
		}
	}
}

// fakeDB answers /api/get like LRCLIB (track, artist and album equal, the
// duration within 2 s) and /api/search with the records whose track name
// contains the query and whose artist name contains the artist.
type fakeDB []Record

func (db fakeDB) get(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dur := 0.0
	if f, err := json.Number(q.Get("duration")).Float64(); err == nil {
		dur = f
	}
	for _, rec := range db {
		if rec.TrackName == q.Get("track_name") && rec.ArtistName == q.Get("artist_name") &&
			rec.AlbumName == q.Get("album_name") && math.Abs(rec.Duration-dur) <= 2 {
			b, _ := json.Marshal(rec)
			jsonBody(string(b))(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

func (db fakeDB) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out := []Record{}
	for _, rec := range db {
		if strings.Contains(rec.TrackName, q.Get("track_name")) && strings.Contains(rec.ArtistName, q.Get("artist_name")) {
			out = append(out, rec)
		}
	}
	b, _ := json.Marshal(out)
	jsonBody(string(b))(w, r)
}

func (db fakeDB) server() *server { return &server{get: db.get, search: db.search} }

func syncedRec(id int64, track, artist, album string, dur float64) Record {
	return Record{ID: id, TrackName: track, ArtistName: artist, AlbumName: album, Duration: dur,
		PlainLyrics: ptr("lời"), SyncedLyrics: ptr("[00:01.00] lời")}
}

// trackQueries lists the requests as "path track_name".
func (s *server) trackQueries() string {
	out := make([]string, len(s.reqs))
	for i, r := range s.reqs {
		out[i] = r.URL.Path + " " + r.URL.Query().Get("track_name")
	}
	return strings.Join(out, "|")
}

// The LRCLIB example: Spotify has the song on an OST album and on a single
// named like the track; LRCLIB has it twice as "假如风有颜色".
func TestLookupDecoratedChineseTitle(t *testing.T) {
	const (
		name = "假如风有颜色(电视剧《白色橄榄树》插曲)"
		core = "假如风有颜色"
	)
	db := fakeDB{
		// As on lrclib.net: the closer entry has plain lyrics only.
		{ID: 36717736, TrackName: core, ArtistName: "卓沅", AlbumName: "假如风有颜色", Duration: 198, PlainLyrics: ptr("lời")},
		syncedRec(38689346, core, "卓沅", name, 197),
		syncedRec(5, core+" (伴奏)", "卓沅", "假如风有颜色", 198),
		syncedRec(6, core+" (Live)", "卓沅", "现场", 198),
	}
	for album, want := range map[string]struct {
		id    int64
		calls string
	}{
		// No LRCLIB album matches: the core search finds both and takes
		// the synced one.
		"白色橄榄树OST": {38689346, "/api/get " + name + "|/api/get " + core + "|/api/search " + core},
		// The single: the core Get finds the entry on that album.
		name: {38689346, "/api/get " + name + "|/api/get " + core},
	} {
		s := db.server()
		l := lookup(t, s, domain.LyricsCandidate{Name: name, Artist: "卓沅", Artists: []string{"卓沅"}, Album: album, DurationMS: 197_500})
		if l == nil || l.LRCLibID != want.id || l.Synced == "" {
			t.Errorf("%s: lyrics = %+v, want %d", album, l, want.id)
		}
		if got := s.trackQueries(); got != want.calls {
			t.Errorf("%s: requests = %q, want %q", album, got, want.calls)
		}
	}
}

func TestLookupDecoratedTitles(t *testing.T) {
	for _, tc := range []struct {
		name, rec string
		artists   []string
	}{
		{"Song (feat. X)", "Song", []string{"A", "X"}},
		{"Song feat. X", "Song", []string{"A"}},
		{"Song - Remastered 2011", "Song", []string{"A"}},
		{"Song (From \"Movie\")", "Song", []string{"A"}},
		{"Song - From \"Movie\"", "Song", []string{"A"}},
		{"Song (with X)", "Song", []string{"A", "X"}},
		{"残酷な天使のテーゼ「新世紀エヴァンゲリオン」", "残酷な天使のテーゼ", []string{"A"}},
		{"『ありがとう』", "ありがとう", []string{"A"}},
		{"ありがとう", "『ありがとう』", []string{"A"}},
		{"Song（TV Size）", "Song", []string{"A"}},
		{"Ｓｏｎｇ", "Song", []string{"A"}},
		{"Don’t Stop", "Don't Stop", []string{"A"}},
	} {
		s := fakeDB{syncedRec(9, tc.rec, "A", "LRCLIB Album", 200)}.server()
		l := lookup(t, s, domain.LyricsCandidate{Name: tc.name, Artist: "A", Artists: tc.artists, Album: "Spotify Album", DurationMS: 200_400})
		if l == nil || l.LRCLibID != 9 {
			t.Errorf("%q: lyrics = %+v, want the %q entry", tc.name, l, tc.rec)
		}
		if len(s.reqs) > MaxRequestsPerLookup {
			t.Errorf("%q: %d requests", tc.name, len(s.reqs))
		}
	}
}

// Version tags must agree: a live track takes the live entry, and with only
// the studio entry it finds nothing (it is retried later); a studio track
// never takes a live or karaoke entry.
func TestLookupVersionTags(t *testing.T) {
	live := domain.LyricsCandidate{Name: "Song (Live)", Artist: "A", Artists: []string{"A"}, Album: "Live Album", DurationMS: 200_000}
	studio := syncedRec(1, "Song", "A", "Studio", 200)
	s := fakeDB{studio, syncedRec(2, "Song - Live", "A", "Other Live", 201)}.server()
	if l := lookup(t, s, live); l == nil || l.LRCLibID != 2 {
		t.Errorf("live track = %+v, want the live entry", l)
	}
	// No Get with "Song": it could only return studio lyrics.
	if got, want := s.trackQueries(), "/api/get Song (Live)|/api/search Song"; got != want {
		t.Errorf("requests = %q, want %q", got, want)
	}
	s = fakeDB{studio}.server()
	if l := lookup(t, s, live); l != nil {
		t.Errorf("live track with only studio lyrics = %+v, want nil", l)
	}
	if got, want := s.trackQueries(), "/api/get Song (Live)|/api/search Song|/api/search Song (Live)"; got != want {
		t.Errorf("requests = %q, want %q", got, want)
	}
	for _, rec := range []string{"Song (Live)", "Song (Karaoke)", "Song (伴奏)", "Song (DJ版)", "Song [Remix]"} {
		s = fakeDB{syncedRec(3, rec, "A", "X", 200)}.server()
		if l := lookup(t, s, domain.LyricsCandidate{Name: "Song", Artist: "A", Album: "Studio", DurationMS: 200_000}); l != nil {
			t.Errorf("studio track took %q: %+v", rec, l)
		}
	}
	// An exact plain match is not replaced by a synced entry of another version.
	plain := Record{ID: 4, TrackName: "Song", ArtistName: "A", AlbumName: "Studio", Duration: 200, PlainLyrics: ptr("p")}
	s = fakeDB{plain, syncedRec(5, "Song (Acoustic)", "A", "X", 200), syncedRec(6, "Song - Remastered", "A", "Y", 200)}.server()
	if l := lookup(t, s, domain.LyricsCandidate{Name: "Song", Artist: "A", Album: "Studio", DurationMS: 200_000}); l == nil || l.LRCLibID != 6 {
		t.Errorf("replacement = %+v, want the synced remaster", l)
	}
}

// A lookup never sends more than MaxRequestsPerLookup requests.
func TestLookupRequestCap(t *testing.T) {
	s := &server{search: jsonBody(`[]`)}
	l := lookup(t, s, domain.LyricsCandidate{Name: "Song (feat. X)", Artist: "A", Artists: []string{"A", "X"},
		Album: "Al", DurationMS: 200_000})
	if l != nil {
		t.Fatalf("lyrics = %+v", l)
	}
	want := "/api/get Song (feat. X)|/api/get Song (feat. X)|/api/get Song|/api/get Song|/api/search Song"
	if got := s.trackQueries(); got != want {
		t.Errorf("requests = %q, want %q", got, want)
	}
	if len(s.reqs) != MaxRequestsPerLookup {
		t.Errorf("requests = %d, want the cap %d", len(s.reqs), MaxRequestsPerLookup)
	}
	// A synced core Get ends the lookup early.
	s = fakeDB{syncedRec(7, "Song", "A, X", "Al", 200)}.server()
	if l := lookup(t, s, domain.LyricsCandidate{Name: "Song (feat. X)", Artist: "A", Artists: []string{"A", "X"},
		Album: "Al", DurationMS: 200_000}); l == nil || l.LRCLibID != 7 || len(s.reqs) != 3 {
		t.Errorf("lyrics = %+v after %d requests, want id 7 after 3", l, len(s.reqs))
	}
}
