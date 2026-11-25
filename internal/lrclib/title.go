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
	// featSuffix is an unbracketed featured-artist part ("Song feat. X").
	featSuffix = regexp.MustCompile(`(?i)\s(?:feat\.?|ft\.?|featuring)\s.+$`)
	// featPart is a removed part that names other artists ("feat. X",
	// "with X"); it carries no version tag.
	featPart = regexp.MustCompile(`^(?:feat\.?|ft\.?|featuring|with)\s`)
)

// versionWords are the version-defining tags, matched as whole words in a
// folded removed part, with their canonical tag.
var versionWords = []struct {
	re  *regexp.Regexp
	tag string
}{
	{regexp.MustCompile(`\b(?:live|concert)\b|现场|現場|演唱会|演唱會|ライブ`), "live"},
	{regexp.MustCompile(`\b(?:acoustic|unplugged)\b|不插电|不插電|アコースティック`), "acoustic"},
	{regexp.MustCompile(`\b(?:remix(?:ed)?|dj|rmx|bootleg)\b|混音|リミックス`), "remix"},
	{regexp.MustCompile(`\b(?:instrumental|inst|karaoke|off vocal|backing track)\b|伴奏|纯音乐|純音樂|カラオケ|インスト`), "inst"},
	{regexp.MustCompile(`\b(?:sped up|speed up|slowed|nightcore)\b|加速|降速`), "speed"},
	{regexp.MustCompile(`\b(?:demo|reprise)\b`), "alt"},
	{regexp.MustCompile(`粤语|粵語|\bcantonese\b`), "lang-yue"},
	{regexp.MustCompile(`国语|國語|普通话|\bmandarin\b`), "lang-cmn"},
	{regexp.MustCompile(`英文版|英语版|英語版|\benglish (?:ver|version)\b`), "lang-en"},
	{regexp.MustCompile(`日文版|日语版|日語版|日本語|\bjapanese (?:ver|version)\b`), "lang-ja"},
	{regexp.MustCompile(`韩文版|韩语版|韓文版|韓語版|\bkorean (?:ver|version)\b`), "lang-ko"},
}

// title is a track name prepared for matching.
type title struct {
	raw      string
	norm     string   // normName of the whole name
	core     string   // normName with decorations removed
	tags     string   // version tags of the removed parts, sorted, "|"-joined
	variants []string // titleVariants
}

func parseTitle(name string) title {
	raw := strings.TrimSpace(name)
	coreRaw, removed := stripAll(norm.NFKC.String(raw))
	return title{raw: raw, norm: normName(raw), core: normName(coreRaw), tags: versionTags(removed), variants: titleVariants(raw)}
}

// match ranks how well a candidate track name matches: 2 for the same
// normName, 1 for the same core with the same version tags, 0 for no match.
func (t title) match(name string) int {
	if t.norm == "" {
		return 0
	}
	o := parseTitle(name)
	switch {
	case o.norm == t.norm:
		return 2
	case o.core == t.core && o.tags == t.tags:
		return 1
	}
	return 0
}

// getVariant is the extra title tried with Get: the most decoration-free
// variant whose version tags equal the name's ("" when none differs). A
// "Song (Live)" has none, since "Song" would only find studio lyrics.
func (t title) getVariant() string {
	for i := len(t.variants) - 1; i > 0; i-- {
		v := t.variants[i]
		if _, removed := stripAll(norm.NFKC.String(v)); versionTags(removed) == t.tags {
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

// titleVariants lists a name, then the name (NFKC, ASCII quotes) with trailing bracketed
// and suffix parts removed, then with every bracketed part removed: no
// blanks, no duplicates. Stripping never empties a name.
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
func stripTrailing(s string) (string, []string) {
	s = collapse(s)
	var removed []string
	for {
		next, part := trimOne(s)
		if next == "" || next == s {
			return s, removed
		}
		s, removed = next, append(removed, part)
	}
}

func trimOne(s string) (string, string) {
	if start, ok := trailingGroup(s); ok && start > 0 {
		_, size := utf8.DecodeRuneInString(s[start:])
		_, last := utf8.DecodeLastRuneInString(s)
		return collapse(s[:start]), s[start+size : len(s)-last]
	}
	if m := dashSep.FindAllStringIndex(s, -1); len(m) > 0 {
		at := m[len(m)-1]
		if p := collapse(s[:at[0]]); p != "" {
			return p, collapse(s[at[1]:])
		}
	}
	if m := tildeSuffix.FindStringSubmatchIndex(s); m != nil && m[0] > 0 {
		return collapse(s[:m[0]]), s[m[2]:m[3]]
	}
	if m := featSuffix.FindStringIndex(s); m != nil {
		return collapse(s[:m[0]]), collapse(s[m[0]:])
	}
	return s, ""
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

// stripAll removes every bracketed group (innermost first), then the
// trailing suffixes. A name that is one bracketed group ("『ありがとう』")
// keeps its inside; a name with nothing left is returned whole with no
// removed parts.
func stripAll(s string) (string, []string) {
	s = collapse(s)
	rest, removed := removeGroups(s)
	rest, more := stripTrailing(rest)
	if rest != "" {
		return rest, append(removed, more...)
	}
	if start, ok := trailingGroup(s); ok && start == 0 {
		_, size := utf8.DecodeRuneInString(s)
		_, last := utf8.DecodeLastRuneInString(s)
		if len(s) > size+last {
			if inner, _ := stripAll(s[size : len(s)-last]); inner != "" {
				return inner, nil
			}
		}
	}
	return s, nil
}

// removeGroups removes the balanced bracketed groups of s and returns them.
func removeGroups(s string) (string, []string) {
	var removed []string
outer:
	for {
		open, openR := -1, rune(0)
		for i, r := range s {
			if _, ok := bracketPairs[r]; ok {
				open, openR = i, r
				continue
			}
			if o, ok := bracketClosers[r]; ok {
				if open >= 0 && o == openR {
					removed = append(removed, s[open+utf8.RuneLen(openR):i])
					s = s[:open] + " " + s[i+utf8.RuneLen(r):]
					continue outer
				}
				open = -1
			}
		}
		return collapse(s), removed
	}
}

// versionTags lists the canonical version tags of removed title parts.
func versionTags(parts []string) string {
	var tags []string
	for _, p := range parts {
		f := collapse(fold(p))
		if featPart.MatchString(f) {
			continue
		}
		for _, w := range versionWords {
			if w.re.MatchString(f) && !slices.Contains(tags, w.tag) {
				tags = append(tags, w.tag)
			}
		}
	}
	slices.Sort(tags)
	return strings.Join(tags, "|")
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
