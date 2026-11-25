package lrc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// LineError is one problem in LRC text: Line is 1-based (0 = the text as a
// whole).
type LineError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// MaxErrors caps the errors Validate returns.
const MaxErrors = 50

// MaxLines caps the timed lines of manual synced lyrics (the public
// now-playing lyrics serve at most 2000).
const MaxLines = 2000

var (
	// leadTag matches one leading bracket group of a line.
	leadTag = regexp.MustCompile(`^\[([^\]]*)\]`)
	// metaTag matches a whole metadata line: [ar:...], [ti:...], [length:...].
	metaTag = regexp.MustCompile(`^\[[A-Za-z#][\w-]*:[^\]]*\]$`)
)

// Normalize cleans text typed or pasted by hand: U+FEFF (byte order marks)
// and NUL bytes are removed, line breaks become "\n", trailing spaces and
// tabs of each line are cut, and leading and trailing blank lines dropped.
func Normalize(text string) string {
	text = strings.ReplaceAll(text, "\uFEFF", "")
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// Validate checks hand-written LRC text (normalized) line by line, with the
// same tag rules as Parse: every non-blank line must be a metadata tag or
// start with at least one valid time tag ([mm:ss], [mm:ss.xx], [mm:ss:xx];
// seconds below 60, minutes at most 999). Lines with one time tag must be in
// time order (a line with several tags, such as a repeated chorus, is not
// checked). With durationMS > 0, no line may start after durationMS + slackMS
// (after the [offset:] shift). The text needs at least one timed line, and
// at most MaxLines. At most MaxErrors errors are returned.
func Validate(text string, durationMS, slackMS int64) []LineError {
	var (
		errs           []LineError
		offset         int64
		timed          int
		prev, prevLine int64 = -1, 0
	)
	add := func(line int, format string, a ...any) {
		if len(errs) < MaxErrors {
			errs = append(errs, LineError{Line: line, Message: fmt.Sprintf(format, a...)})
		}
	}
	lines := strings.Split(text, "\n")
	for _, raw := range lines {
		if m := offsetTag.FindStringSubmatch(strings.TrimSpace(raw)); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				offset = n
			}
		}
	}
	for i, raw := range lines {
		n := i + 1
		s := strings.TrimSpace(raw)
		if s == "" || offsetTag.MatchString(s) {
			continue
		}
		var starts []int64
		bad := false
		for {
			m := leadTag.FindStringSubmatchIndex(s)
			if m == nil {
				break
			}
			inner := s[m[2]:m[3]]
			if t := timeTag.FindStringSubmatchIndex(s); t != nil && t[1] == m[1] {
				ms, ok := tagMS(s, t)
				if !ok {
					add(n, "invalid timestamp [%s]: seconds must be below 60 and minutes at most %d", inner, maxMinutes)
					bad = true
				} else {
					starts = append(starts, ms)
				}
				s = s[m[1]:]
				continue
			}
			if inner != "" && unicode.IsDigit(rune(inner[0])) {
				add(n, "invalid timestamp [%s]: use [mm:ss.xx]", inner)
				bad = true
			}
			break
		}
		if bad {
			continue
		}
		if len(starts) == 0 {
			if metaTag.MatchString(s) {
				continue
			}
			add(n, "no timestamp: start the line with [mm:ss.xx], or remove it")
			continue
		}
		timed += len(starts)
		if len(starts) == 1 {
			if prev >= 0 && starts[0] < prev {
				add(n, "out of order: %s comes before %s on line %d", formatMS(starts[0]), formatMS(prev), prevLine)
			} else {
				prev, prevLine = starts[0], int64(n)
			}
		}
		if durationMS > 0 {
			for _, ms := range starts {
				if at := max(0, ms-offset); at > durationMS+slackMS {
					add(n, "%s is past the end of the song (%s)", formatMS(at), formatMS(durationMS))
					break
				}
			}
		}
	}
	if timed > MaxLines {
		add(0, "too many timed lines (%d; at most %d)", timed, MaxLines)
	}
	if timed == 0 && len(errs) == 0 {
		add(0, "no timed lines: synced lyrics need [mm:ss.xx] timestamps")
	}
	return errs
}

// Plain derives plain lyrics from LRC text: metadata lines are dropped, time
// and word tags removed, and lines that hold no words (empty timed lines,
// "♪", "[Instrumental]") become stanza breaks. Runs of blank lines collapse
// to one, and the text is trimmed. Lines keep file order.
func Plain(synced string) string {
	var out []string
	blank := false
	for raw := range strings.SplitSeq(synced, "\n") {
		s := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if offsetTag.MatchString(s) || metaTag.MatchString(s) {
			continue
		}
		for {
			m := timeTag.FindStringIndex(s)
			if m == nil {
				break
			}
			s = strings.TrimSpace(s[m[1]:])
		}
		s = strings.TrimSpace(wordTag.ReplaceAllString(s, ""))
		if MarkerOnly(s) {
			blank = len(out) > 0
			continue
		}
		if blank {
			out = append(out, "")
			blank = false
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n")
}

// MarkerOnly reports whether text holds no lyrics: nothing but symbols
// ("♪", "..."), and the word "instrumental" ("[Instrumental]").
func MarkerOnly(s string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return strings.ReplaceAll(b.String(), "instrumental", "") == ""
}

// formatMS formats ms as m:ss.xx.
func formatMS(ms int64) string {
	return fmt.Sprintf("%d:%02d.%02d", ms/60_000, ms/1000%60, ms%1000/10)
}
