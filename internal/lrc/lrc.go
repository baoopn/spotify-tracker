// Package lrc parses LRC (time-synced lyrics) text. It has no dependencies.
package lrc

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Line is one timed lyrics line. An empty Text marks an instrumental gap.
type Line struct {
	StartMS int64  `json:"start_ms"`
	Text    string `json:"text"`
}

var (
	// timeTag matches one leading time tag: [mm:ss], [mm:ss.f..fff] or [mm:ss:ff].
	timeTag   = regexp.MustCompile(`^\[(\d+):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	offsetTag = regexp.MustCompile(`(?i)^\[offset:\s*([+-]?\d+)\s*\]$`)
	wordTag   = regexp.MustCompile(`<\d+:\d{1,2}(?:[.:]\d{1,3})?>`)
)

// Parse returns the timed lines of text, sorted by StartMS (file order on
// ties). Lines without a time tag and metadata tags ([ar:], [ti:], ...) are
// ignored; [offset:N] (any case) shifts every line N ms earlier (clamped at
// 0). U+FEFF (byte order mark) is removed.
func Parse(text string) []Line {
	var (
		lines  []Line
		offset int64
	)
	text = strings.ReplaceAll(text, "\uFEFF", "")
	for raw := range strings.SplitSeq(text, "\n") {
		s := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if s == "" {
			continue
		}
		if m := offsetTag.FindStringSubmatch(s); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				offset = n
			}
			continue
		}
		var starts []int64
		for {
			m := timeTag.FindStringSubmatchIndex(s)
			if m == nil {
				break
			}
			if ms, ok := tagMS(s, m); ok {
				starts = append(starts, ms)
			}
			s = s[m[1]:]
		}
		if len(starts) == 0 {
			continue
		}
		body := strings.TrimSpace(wordTag.ReplaceAllString(s, ""))
		for _, ms := range starts {
			lines = append(lines, Line{StartMS: ms, Text: body})
		}
	}
	for i := range lines {
		lines[i].StartMS = max(0, lines[i].StartMS-offset)
	}
	sort.SliceStable(lines, func(a, b int) bool { return lines[a].StartMS < lines[b].StartMS })
	return lines
}

// maxMinutes is the largest minute value of a valid time tag (web/admin
// src/lib/lrc.ts uses the same rules).
const maxMinutes = 999

// tagMS converts the submatches of one time tag (indexes m into s). An
// invalid tag (seconds >= 60, minutes > maxMinutes) is consumed and dropped.
func tagMS(s string, m []int) (int64, bool) {
	minutes, err := strconv.ParseInt(s[m[2]:m[3]], 10, 64)
	if err != nil || minutes > maxMinutes {
		return 0, false
	}
	secs, err := strconv.ParseInt(s[m[4]:m[5]], 10, 64)
	if err != nil || secs >= 60 {
		return 0, false
	}
	var frac int64
	if m[6] >= 0 {
		f := s[m[6]:m[7]]
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return 0, false
		}
		switch len(f) {
		case 1:
			frac = n * 100
		case 2:
			frac = n * 10
		default:
			frac = n
		}
	}
	return minutes*60_000 + secs*1000 + frac, true
}

// CutRunes returns s cut to at most n runes (n <= 0: no cut).
func CutRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
