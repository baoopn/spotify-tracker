package lrclib

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// tagPhrases maps the version-defining phrases (as normName writes them) to
// their canonical tag. A removed title part carries tags only when it is
// made of such phrases, apart from "version"/"ver"/"版", a year, a "live at
// …" place and a remixer's name before "remix": "(Live)", "- Live at
// Wembley", "(Acoustic Version)", "(Kygo Remix)", "(DJ版)", "(现场版)". So
// "- Live Forever" or "(From "Love Live!")" carry none.
var tagPhrases = map[string]string{
	"live": "live", "concert": "live", "现场": "live", "現場": "live", "演唱会": "live",
	"演唱會": "live", "ライブ": "live",
	"acoustic": "acoustic", "unplugged": "acoustic", "不插电": "acoustic", "不插電": "acoustic",
	"アコースティック": "acoustic",
	"piano":    "piano", "钢琴": "piano", "鋼琴": "piano", "ピアノ": "piano",
	"remix": "remix", "remixed": "remix", "rmx": "remix", "dj": "remix", "bootleg": "remix",
	"混音": "remix", "リミックス": "remix",
	"instrumental": "inst", "inst": "inst", "karaoke": "inst", "off vocal": "inst",
	"backing track": "inst", "beat": "inst", "伴奏": "inst", "纯音乐": "inst", "純音樂": "inst",
	"カラオケ": "inst", "インスト": "inst",
	"sped up": "speed", "speed up": "speed", "slowed": "speed", "slowed reverb": "speed",
	"nightcore": "speed", "加速": "speed", "降速": "speed",
	"demo": "alt", "reprise": "alt",
	"cantonese": "lang-yue", "粤语": "lang-yue", "粵語": "lang-yue",
	"mandarin": "lang-cmn", "国语": "lang-cmn", "國語": "lang-cmn", "普通话": "lang-cmn",
	"chinese": "lang-zh", "中文": "lang-zh",
	"english": "lang-en", "英文": "lang-en", "英语": "lang-en", "英語": "lang-en",
	"japanese": "lang-ja", "日文": "lang-ja", "日语": "lang-ja", "日語": "lang-ja", "日本語": "lang-ja",
	"korean": "lang-ko", "韩文": "lang-ko", "韓文": "lang-ko", "韩语": "lang-ko", "韓語": "lang-ko",
}

// tagOrder lists tagPhrases longest first, so "off vocal" wins over shorter
// prefixes.
var tagOrder = func() []string {
	out := make([]string, 0, len(tagPhrases))
	for p := range tagPhrases {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}()

var (
	// featPart is a removed part that names other artists ("feat. X",
	// "with X", "prod. X"); it carries no version tag and is harmless.
	featPart = regexp.MustCompile(`^(?:feat|ft|featuring|with|prod|produced by)\s`)
	// tagFiller are the words a tag part may carry besides its phrases.
	tagFiller = regexp.MustCompile(`\b(?:version|ver|edition|\d{4})\b|版`)
	// liveAt is "live at/from/in/on <place>".
	liveAt = regexp.MustCompile(`^(.*\blive) (?:at|from|in|on) .+$`)
	// remixBy is "<remixer> remix".
	remixBy = regexp.MustCompile(`^.+ (remix|rmx)$`)
	// harmlessText is a decoration that does not change the song: a
	// remaster, a film or drama credit, a radio edit, an explicit or clean
	// or single version.
	harmlessText = regexp.MustCompile(`^(?:\d{4} )?(?:digital(?:ly)? )?remaster(?:ed)?(?: \d{4})?(?: version| edition)?$|` +
		`^from\b|\bost\b|original soundtrack|插曲|主题曲|主題曲|片尾曲|片头曲|片頭曲|电视剧|電視劇|\bdrama\b|^tv size$|` +
		`^radio edit$|^(?:explicit|clean)(?: version)?$|^single (?:version|edit)$`)
)

// partText is the text of a removed part that tags are read from: groups
// nested in a bracketed part (a 《drama title》 inside "(…)") are dropped.
func partText(p part) string {
	if p.open != 0 && p.open != '~' {
		rest, _ := topGroups(p.text)
		return normName(rest)
	}
	return normName(p.text)
}

// partTags lists the version tags of a removed part, nil when the part is
// not made of tag phrases.
func partTags(p part) []string {
	n := partText(p)
	if n == "" || featPart.MatchString(n) {
		return nil
	}
	n = collapse(tagFiller.ReplaceAllString(n, " "))
	if m := liveAt.FindStringSubmatch(n); m != nil {
		n = m[1]
	}
	if m := remixBy.FindStringSubmatch(n); m != nil {
		n = m[1]
	}
	var tags []string
	for n != "" {
		found := false
		for _, ph := range tagOrder {
			rest, ok := strings.CutPrefix(n, ph)
			if !ok {
				continue
			}
			// An ASCII phrase must end a word ("live" not in "lively").
			if r, _ := utf8.DecodeRuneInString(rest); rest != "" && r < utf8.RuneSelf && r != ' ' {
				continue
			}
			tags, n, found = append(tags, tagPhrases[ph]), strings.TrimSpace(rest), true
			break
		}
		if !found {
			return nil
		}
	}
	return tags
}

// versionTags lists the canonical version tags of removed title parts.
func versionTags(parts []part) string {
	var tags []string
	for _, p := range parts {
		for _, t := range partTags(p) {
			if !slices.Contains(tags, t) {
				tags = append(tags, t)
			}
		}
	}
	slices.Sort(tags)
	return strings.Join(tags, "|")
}

// harmlessParts reports whether every removed part is a decoration that
// does not change the song: a featured artist, a remaster, a film or drama
// credit, a 《drama title》, a radio edit, an explicit, clean or single
// version.
func harmlessParts(parts []part) bool {
	for _, p := range parts {
		n := normName(p.text)
		if p.open != '《' && !featPart.MatchString(n) && !harmlessText.MatchString(n) {
			return false
		}
	}
	return true
}
