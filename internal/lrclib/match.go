package lrclib

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// MaxRequestsPerLookup is the most requests one Lookup sends: a Get with all
// artists, a Get with the first artist and a search. config checks
// LYRICS_LEASE against it.
const MaxRequestsPerLookup = 3

// minContain is the shortest artist name (in runes) that may match as part
// of a longer one; shorter names ("V", "Ni") must match a whole name.
const minContain = 3

// Lookup finds the lyrics of a track. With an album and a duration it first
// tries the exact Get with all credited artists joined by ", " (as LRCLIB
// writes them), then with the first artist only (skipped for one artist),
// then searches. It stops at the first result with synced lyrics. An exact
// match with only plain lyrics, or instrumental, is kept while the search
// looks for a synced candidate with the exact folded name, the first artist
// and the duration; without one the exact match is returned. A Get that
// fails (other than a rate limit or a cancelled ctx) does not end the
// lookup: when nothing is found, its error is returned so the track is
// retried. nil, nil means no lyrics were found.
func (c *Client) Lookup(ctx context.Context, cand domain.LyricsCandidate) (*domain.Lyrics, error) {
	artists := credited(cand)
	if len(artists) == 0 {
		return nil, nil
	}
	durSec := int(math.Round(float64(cand.DurationMS) / 1000))
	var exact *Record // the best exact match without synced lyrics
	var getErr error  // a Get that failed; the lookup goes on
	if cand.Album != "" && durSec > 0 {
		names := []string{strings.Join(artists, ", ")}
		if len(artists) > 1 {
			names = append(names, artists[0])
		}
		for _, artist := range names {
			rec, err := c.Get(ctx, cand.Name, artist, cand.Album, durSec)
			if err != nil {
				if stop(ctx, err) {
					return nil, err
				}
				getErr = err
				continue
			}
			if rec == nil {
				continue
			}
			if lyricsKind(rec) == kindSynced {
				return toLyrics(rec), nil
			}
			if exact == nil || lyricsKind(rec) > lyricsKind(exact) {
				exact = rec
			}
		}
	}
	recs, err := c.Search(ctx, cand.Name, artists[0])
	if err != nil {
		if exact == nil || stop(ctx, err) {
			return nil, err
		}
		return toLyrics(exact), nil // keep the exact match (e.g. on a timeout)
	}
	if exact == nil {
		if rec := pick(recs, cand.Name, artists, durSec, nil); rec != nil {
			return toLyrics(rec), nil
		}
		return nil, getErr
	}
	// Only synced lyrics replace an exact match, also an instrumental one.
	want := fold(cand.Name)
	if rec := pick(recs, cand.Name, artists, durSec, func(r *Record) bool {
		return fold(r.TrackName) == want && lyricsKind(r) == kindSynced && primaryMatch(r.ArtistName, artists[0])
	}); rec != nil {
		return toLyrics(rec), nil
	}
	return toLyrics(exact), nil
}

// stop reports whether err ends the lookup at once: a rate limit (the whole
// lookup is retried after it) or a cancelled ctx. An HTTP client timeout is
// not a cancelled ctx.
func stop(ctx context.Context, err error) bool {
	var rl *RateLimitError
	return ctx.Err() != nil || errors.As(err, &rl)
}

// credited lists the candidate's artists in credit order, without blanks
// (Artist alone when Artists is empty).
func credited(cand domain.LyricsCandidate) []string {
	src := cand.Artists
	if len(src) == 0 {
		src = []string{cand.Artist}
	}
	out := make([]string, 0, len(src))
	for _, a := range src {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// Kinds of lyrics a record holds, as toLyrics would store them.
const (
	kindNone   = iota // instrumental, or no lyrics text
	kindPlain         // plain lyrics only
	kindSynced        // synced lyrics
)

func lyricsKind(r *Record) int {
	l := toLyrics(r)
	switch {
	case l.Status != domain.LyricsFound:
		return kindNone
	case l.Synced != "":
		return kindSynced
	default:
		return kindPlain
	}
}

// pick chooses the best search candidate. A candidate must have the same
// track name (folded, ignoring a bracketed or " - " version suffix such as
// "(Live)" or "- Remastered"), an artist that matches one of the credited
// artists (artistMatch), and, when the duration is known, a duration within
// durationSlack; accept (nil = any) may narrow it further. Among those it
// prefers the exact folded name, then synced lyrics, then plain lyrics, then
// the closest duration.
func pick(recs []Record, name string, artists []string, durSec int, accept func(*Record) bool) *Record {
	want, wantBase := fold(name), baseName(name)
	var wantNames []string
	for _, a := range artists {
		wantNames = append(wantNames, artistNames(a)...)
	}
	var best *Record
	var bestKey [4]float64
	for i := range recs {
		r := &recs[i]
		if r.ID == 0 || baseName(r.TrackName) != wantBase || !artistMatch(artistNames(r.ArtistName), wantNames) {
			continue
		}
		if accept != nil && !accept(r) {
			continue
		}
		diff := 0.0
		if durSec > 0 {
			if diff = math.Abs(r.Duration - float64(durSec)); diff > durationSlack {
				continue
			}
		}
		key := [4]float64{b2f(fold(r.TrackName) == want), b2f(nonBlank(r.SyncedLyrics)), b2f(nonBlank(r.PlainLyrics)), -diff}
		if best == nil || less(bestKey, key) {
			best, bestKey = r, key
		}
	}
	return best
}

// artistSep splits a folded artist credit into names: ",", "&", ";",
// "feat.", "ft.", "featuring" and "x".
var artistSep = regexp.MustCompile(`\s*(?:[,&;]|\s(?:feat\.?|ft\.?|featuring|x)\s)\s*`)

// artistNames splits an artist credit into folded names, spaces collapsed
// ("Đen feat. MIN" → "den", "min").
func artistNames(s string) []string {
	f := strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ").Replace(fold(s))
	var out []string
	for _, p := range artistSep.Split(f, -1) {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// nameMatch reports whether two folded names are the same artist: equal, or
// the shorter (at least minContain runes) is whole words of the longer
// ("son tung" in "son tung m-tp").
func nameMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if utf8.RuneCountInString(a) > utf8.RuneCountInString(b) {
		a, b = b, a
	}
	return utf8.RuneCountInString(a) >= minContain && strings.Contains(" "+b+" ", " "+a+" ")
}

// artistMatch reports whether any candidate name matches any wanted name.
func artistMatch(got, want []string) bool {
	for _, g := range got {
		for _, w := range want {
			if nameMatch(g, w) {
				return true
			}
		}
	}
	return false
}

// primaryMatch reports whether a candidate credit names the first credited
// artist (every name of it, when it is itself a credit like "A & B"): a
// candidate name equal to it, or containing it as whole words (at least
// minContain runes). A candidate name that is only part of it ("Bình" for
// "Vương Bình") does not count. The joined credit always contains the first
// artist, so it matches too.
func primaryMatch(credit, first string) bool {
	got := artistNames(credit)
	names := artistNames(first)
	for _, n := range names {
		ok := false
		for _, g := range got {
			if g == n || (utf8.RuneCountInString(n) >= minContain && strings.Contains(" "+g+" ", " "+n+" ")) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return len(names) > 0
}

// nameSuffix matches a trailing "(…)", "[…]" or " - …" version suffix.
var nameSuffix = regexp.MustCompile(`\s*(?:\([^)]*\)|\[[^\]]*\]|\s-\s.*)$`)

// baseName folds a track name and removes version suffixes ("(Live)",
// "[Remix]", "- Remastered 2011").
func baseName(s string) string {
	b := fold(s)
	for {
		next := strings.TrimSpace(nameSuffix.ReplaceAllString(b, ""))
		if next == b || next == "" {
			return b
		}
		b = next
	}
}

func less(a, b [4]float64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func nonBlank(s *string) bool { return s != nil && strings.TrimSpace(*s) != "" }

var lrcStamp = regexp.MustCompile(`^\[\d+:\d+(?:\.\d+)?\]\s*`)

// cleanText removes NUL bytes (Postgres text rejects them), normalizes line
// breaks to "\n" and trims the text.
func cleanText(s *string) string {
	if s == nil {
		return ""
	}
	t := strings.ReplaceAll(*s, "\x00", "")
	t = strings.ReplaceAll(t, "\r\n", "\n")
	t = strings.ReplaceAll(t, "\r", "\n")
	return strings.TrimSpace(t)
}

// unstamp removes the LRC timestamps of synced text.
func unstamp(synced string) string {
	lines := strings.Split(synced, "\n")
	for i, l := range lines {
		lines[i] = lrcStamp.ReplaceAllString(l, "")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// markerOnly reports whether text holds no lyrics: nothing but symbols
// ("♪", "..."), and the word "instrumental" ("[Instrumental]").
func markerOnly(s string) bool {
	var b strings.Builder
	for _, r := range fold(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return strings.ReplaceAll(b.String(), "instrumental", "") == ""
}

// toLyrics maps a record. It is instrumental when flagged or when no lyrics
// text is left (marker-only text such as "[Instrumental]" or "♪" counts as
// none); plain text is derived from synced text when missing.
func toLyrics(r *Record) *domain.Lyrics {
	plain, synced := cleanText(r.PlainLyrics), cleanText(r.SyncedLyrics)
	words := unstamp(synced)
	if markerOnly(words) {
		synced, words = "", ""
	}
	if markerOnly(plain) {
		plain = ""
	}
	if plain == "" {
		plain = words
	}
	if r.Instrumental || plain == "" {
		return &domain.Lyrics{Status: domain.LyricsInstrumental, LRCLibID: r.ID}
	}
	return &domain.Lyrics{Status: domain.LyricsFound, LRCLibID: r.ID, Plain: capText(plain), Synced: capText(synced)}
}

// capText cuts s to at most maxText bytes, at the last newline before the cap.
func capText(s string) string {
	if len(s) <= maxText {
		return s
	}
	cut := s[:maxText]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		return cut[:i]
	}
	// No newline: cut at a rune boundary.
	for i := maxText; i > 0; i-- {
		if (s[i] & 0xC0) != 0x80 {
			return s[:i]
		}
	}
	return ""
}

// foldTable maps accented lowercase letters (Vietnamese and common Latin)
// to their base letter.
var foldTable = func() map[rune]rune {
	m := map[rune]rune{}
	for base, set := range map[rune]string{
		'a': "àáảãạăằắẳẵặâầấẩẫậäåā", 'e': "èéẻẽẹêềếểễệëē", 'i': "ìíỉĩịïî",
		'o': "òóỏõọôồốổỗộơờớởỡợöøō", 'u': "ùúủũụưừứửữựüûū", 'y': "ỳýỷỹỵÿ",
		'd': "đð", 'c': "ç", 'n': "ñ", 'l': "ł",
	} {
		for _, r := range set {
			m[r] = base
		}
	}
	return m
}()

// fold lowercases s and removes accents, close to search_fold in SQL; it is
// used to compare track and artist names.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if r >= 0x300 && r <= 0x36f { // combining marks of decomposed text
			continue
		}
		if f, ok := foldTable[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}
