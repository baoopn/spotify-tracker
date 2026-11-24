// Package lrc parses LRC (time-synced lyrics) text and cuts short windows of
// lines around a playback position. It has no dependencies.
package lrc

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

// WindowOptions bounds a Window.
type WindowOptions struct {
	Ahead    time.Duration // lines starting up to this far after the position
	MaxLines int           // hard cap on returned lines (at least 3)
	MaxRunes int           // each line's text is cut to this many runes (0 = no cut)
}

// Window is a short run of lines around a position.
type Window struct {
	FromMS  int64  // start of the current line (0 before the first line)
	UntilMS *int64 // start of the first line not included; nil when none remain
	Lines   []Line
}

// Cut returns the previous line, the current line (the last one starting at
// or before posMS), the next line, and further lines starting within
// opt.Ahead of posMS, never more than opt.MaxLines.
func Cut(lines []Line, posMS int64, opt WindowOptions) Window {
	maxLines := max(opt.MaxLines, 3)
	i := sort.Search(len(lines), func(k int) bool { return lines[k].StartMS > posMS }) - 1
	w := Window{}
	if i >= 0 {
		w.FromMS = lines[i].StartMS
	}
	limit := posMS + opt.Ahead.Milliseconds()
	j := max(0, i-1)
	for ; j < len(lines) && len(w.Lines) < maxLines; j++ {
		if j > i+1 && lines[j].StartMS > limit {
			break
		}
		l := lines[j]
		l.Text = cutRunes(l.Text, opt.MaxRunes)
		w.Lines = append(w.Lines, l)
	}
	if j < len(lines) {
		u := lines[j].StartMS
		w.UntilMS = &u
	}
	return w
}

func cutRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
