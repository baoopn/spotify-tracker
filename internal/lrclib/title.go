package lrclib

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Track titles often carry decorations that LRCLIB leaves out, or the other
// way round: "假如风有颜色(电视剧《白色橄榄树》插曲)" is "假如风有颜色" there.
// A title is compared in two forms: normName (NFKC, folded, punctuation and
// spaces collapsed) and its core, the normName with every bracketed part and
// every trailing " - …", "~…~" and "feat. …" suffix removed. Version tags
// (Live, Remix, Instrumental, 伴奏, …) found in the removed parts must agree
// on both sides, so a live or karaoke track never takes the studio lyrics.

// bracketPairs maps each opening bracket to its closing one (after NFKC,
// which turns full-width （）［］｛｝ into ASCII).
var bracketPairs = map[rune]rune{
	'(': ')', '[': ']', '{': '}', '【': '】', '「': '」', '『': '』',
	'《': '》', '〈': '〉', '〔': '〕', '〖': '〗', '«': '»',
}

var bracketClosers = func() map[rune]rune {
	m := map[rune]rune{}
	for o, c := range bracketPairs {
		m[c] = o
	}
	return m
}()

var (
	// dashSep is a spaced dash before a suffix (" - Remastered 2011").
	dashSep = regexp.MustCompile(`\s[-–—]\s`)
	// tildeSuffix is a trailing "~…~" subtitle (Japanese style).
	tildeSuffix = regexp.MustCompile(`\s*[~〜]([^~〜]+)[~〜]$`)
	// featSuffix is an unbracketed featured-artist part followed by a name
	// ("Song feat. X", not "5 ft. 7").
	featSuffix = regexp.MustCompile(`(?i)\s(?:feat\.?|ft\.?|featuring)\s+\pL.*$`)
)

// part is a removed piece of a title: the inside of a bracketed group
// (open is its opening bracket) or a suffix (open 0, or '~').
type part struct {
	text string
	open rune
}

// title is a track name prepared for matching.
type title struct {
	raw      string
	norm     string   // normName of the whole name
	core     string   // normName with decorations removed
	tags     string   // version tags of the removed parts, sorted, "|"-joined
	harmless bool     // every removed part is a harmless decoration
	variants []string // titleVariants
}

func parseTitle(name string) title {
	raw := strings.TrimSpace(name)
	coreRaw, parts := stripAll(norm.NFKC.String(raw))
	return title{raw: raw, norm: normName(raw), core: normName(coreRaw), tags: versionTags(parts),
		harmless: harmlessParts(parts), variants: titleVariants(raw)}
}

// match ranks how well a candidate track name matches: 2 for the same
// normName, 1 for the same core with the same version tags, 0 for no match.
func (t title) match(name string) int {
	if t.norm == "" {
		return 0
	}
	return t.matchTitle(parseTitle(name))
}

func (t title) matchTitle(o title) int {
	switch {
	case o.norm == t.norm:
		return 2
	case o.core == t.core && o.tags == t.tags:
		return 1
	}
	return 0
}

// replaces reports whether a candidate name may replace an exact match: the
// same normName, or the same core when both sides removed only harmless
// decorations ("- Remastered", "(电视剧《…》插曲)", "feat. X"). "Intro" is not
// replaced by "Intro (Song A)", nor "Interlude - A" by "Interlude - B".
func (t title) replaces(name string) bool {
	if t.norm == "" {
		return false
	}
	o := parseTitle(name)
	switch t.matchTitle(o) {
	case 2:
		return true
	case 1:
		return t.harmless && o.harmless
	}
	return false
}

// getVariant is the extra title tried with Get: the most decoration-free
// variant whose version tags equal the name's ("" when none differs). A
// "Song (Live)" has none, since "Song" would only find studio lyrics.
func (t title) getVariant() string {
	for i := len(t.variants) - 1; i > 0; i-- {
		v := t.variants[i]
		if _, parts := stripAll(norm.NFKC.String(v)); versionTags(parts) == t.tags {
			return v
		}
	}
	return ""
}

// searchTitles are the search queries: the core variant, then the name.
func (t title) searchTitles() []string {
	if len(t.variants) < 2 {
		return t.variants
	}
	return []string{t.variants[len(t.variants)-1], t.raw}
}

// quotes turns typographic quotes into ASCII ones, as most LRCLIB names
// have them.
var quotes = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`)

// titleVariants lists a name, then the name (NFKC, ASCII quotes) with
// trailing bracketed and suffix parts removed, then with every bracketed
// part removed: no blanks, no duplicates. Stripping never empties a name.
func titleVariants(name string) []string {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return nil
	}
	out := []string{raw}
	n := quotes.Replace(norm.NFKC.String(raw))
	trailing, _ := stripTrailing(n)
	all, _ := stripAll(n)
	for _, v := range []string{trailing, all} {
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// stripTrailing removes trailing decorations one at a time: a bracketed
// group, a " - …" suffix, a "~…~" subtitle, a "feat. …" part. It stops
// before the name would be empty and returns the removed parts.
func stripTrailing(s string) (string, []part) {
	s = collapse(s)
	var removed []part
	for {
		next, p := trimOne(s)
		if next == "" || next == s {
			return s, removed
		}
		s, removed = next, append(removed, p)
	}
}

func trimOne(s string) (string, part) {
	if start, ok := trailingGroup(s); ok && start > 0 {
		open, size := utf8.DecodeRuneInString(s[start:])
		_, last := utf8.DecodeLastRuneInString(s)
		return collapse(s[:start]), part{text: s[start+size : len(s)-last], open: open}
	}
	if m := dashSep.FindAllStringIndex(s, -1); len(m) > 0 {
		at := m[len(m)-1]
		if p := collapse(s[:at[0]]); p != "" {
			return p, part{text: collapse(s[at[1]:])}
		}
	}
	if m := tildeSuffix.FindStringSubmatchIndex(s); m != nil && m[0] > 0 {
		return collapse(s[:m[0]]), part{text: s[m[2]:m[3]], open: '~'}
	}
	if m := featSuffix.FindStringIndex(s); m != nil {
		return collapse(s[:m[0]]), part{text: collapse(s[m[0]:])}
	}
	return s, part{}
}

// trailingGroup finds the opening bracket of the group that ends s.
func trailingGroup(s string) (int, bool) {
	closeR, _ := utf8.DecodeLastRuneInString(s)
	openR, ok := bracketClosers[closeR]
	if !ok {
		return 0, false
	}
	depth := 0
	for i := len(s); i > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		i -= size
		switch r {
		case closeR:
			depth++
		case openR:
			if depth--; depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// stripAll removes every top-level bracketed group, then the trailing
// suffixes. A name made only of bracketed groups ("『ありがとう』",
// "《X》(Live)") takes the inside of the first non-empty group as its title
// and the other groups as removed parts; a name with nothing left is
// returned whole with no removed parts.
func stripAll(s string) (string, []part) {
	s = collapse(s)
	rest, parts := topGroups(s)
	rest, more := stripTrailing(rest)
	if rest != "" {
		return rest, append(parts, more...)
	}
	for i, p := range parts {
		if core, inner := stripAll(p.text); core != "" {
			return core, append(inner, parts[i+1:]...)
		}
	}
	return s, nil
}

// topGroups removes the balanced top-level bracketed groups of s and
// returns them; groups nested inside them stay in their text. An unclosed
// group stays in the name.
func topGroups(s string) (string, []part) {
	var b strings.Builder
	var parts []part
	var stack []rune
	start := 0
	for i, r := range s {
		if _, ok := bracketPairs[r]; ok {
			if len(stack) == 0 {
				start = i
			}
			stack = append(stack, r)
			continue
		}
		if o, ok := bracketClosers[r]; ok && len(stack) > 0 {
			if stack[len(stack)-1] == o {
				if stack = stack[:len(stack)-1]; len(stack) == 0 {
					open, size := utf8.DecodeRuneInString(s[start:])
					parts = append(parts, part{text: s[start+size : i], open: open})
					b.WriteByte(' ')
				}
			}
			continue
		}
		if len(stack) == 0 {
			b.WriteRune(r)
		}
	}
	if len(stack) > 0 {
		b.WriteString(s[start:])
	}
	return collapse(b.String()), parts
}

// normName folds a name for comparison: NFKC (full-width to half-width),
// lowercase without accents (fold), apostrophes dropped ("Don’t" = "Dont"),
// other punctuation and symbols as spaces, spaces collapsed. A name of only
// punctuation keeps it.
func normName(s string) string {
	f := fold(norm.NFKC.String(s))
	var b strings.Builder
	gap := false
	for _, r := range f {
		switch {
		case strings.ContainsRune(`'’‘´`+"`", r):
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			if gap && b.Len() > 0 {
				b.WriteByte(' ')
			}
			gap = false
			b.WriteRune(r)
		default:
			gap = true
		}
	}
	if b.Len() == 0 {
		return collapse(f)
	}
	return b.String()
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
