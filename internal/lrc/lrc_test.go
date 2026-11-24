package lrc

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Line
	}{
		{"empty", "", nil},
		{"multi tags", "[00:01.00][00:30.00] chorus", []Line{{1000, "chorus"}, {30000, "chorus"}}},
		{"fraction widths", "[00:01] a\n[00:02.5] b\n[00:03.05] c\n[00:04.005] d\n[00:05:25] e",
			[]Line{{1000, "a"}, {2500, "b"}, {3050, "c"}, {4005, "d"}, {5250, "e"}}},
		{"three-digit minutes", "[100:00.00] late", []Line{{6_000_000, "late"}}},
		{"offset positive", "[offset:+500]\n[00:01.00] a\n[00:00.20] b", []Line{{0, "b"}, {500, "a"}}},
		{"offset negative", "[00:01.00] a\n[offset:-500]", []Line{{1500, "a"}}},
		{"metadata dropped", "[ar:Artist]\n[ti:Title]\n[al:Album]\n[by:x]\n[length:03:20]\n[re:y]\n[ve:1]\n[00:01.00] a",
			[]Line{{1000, "a"}}},
		{"blank timed kept, untimed dropped", "[00:01.00] a\n\n   \nplain words\n[00:02.00]\n[00:03.00] b",
			[]Line{{1000, "a"}, {2000, ""}, {3000, "b"}}},
		{"unsorted stable", "[00:03.00] c\n[00:01.00] a\n[00:01.00] a2\n[00:02.00] b",
			[]Line{{1000, "a"}, {1000, "a2"}, {2000, "b"}, {3000, "c"}}},
		{"crlf", "[00:01.00] a\r\n[00:02.00] b\r\n", []Line{{1000, "a"}, {2000, "b"}}},
		{"word tags", "[00:01.00] <00:01.00>hel <00:01.50>lo", []Line{{1000, "hel lo"}}},
		{"garbage", "[99:99.99] x\n[ab:cd] y\n[::] z\n[00:01.00", nil},
		// Shared with web/admin scripts/check-lrc.mjs: keep the two in sync.
		{"invalid second tag dropped", "[00:01.00][00:75.00] x", []Line{{1000, "x"}}},
		{"byte order mark", "\uFEFF[00:01.00] a\n[00:02.00] b", []Line{{1000, "a"}, {2000, "b"}}},
		{"offset any case", "[OFFSET:+500]\n[00:01.00] a", []Line{{500, "a"}}},
		{"minutes cap", "[1000:00.00] x\n[999:59.99] y", []Line{{59_999_990, "y"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Parse(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Parse(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func seq(n int, stepMS int64) []Line {
	out := make([]Line, n)
	for i := range out {
		out[i] = Line{StartMS: int64(i+1) * stepMS, Text: fmt.Sprintf("line %d", i)}
	}
	return out
}

func starts(w Window) []int64 {
	var s []int64
	for _, l := range w.Lines {
		s = append(s, l.StartMS)
	}
	return s
}

func TestCut(t *testing.T) {
	opt := WindowOptions{Ahead: 30 * time.Second, MaxLines: 10, MaxRunes: 200}
	lines := seq(20, 10_000) // 10 s, 20 s, ... 200 s

	// Before the first line: no current line, next line included.
	w := Cut(lines, 500, opt)
	if w.FromMS != 0 || !reflect.DeepEqual(starts(w), []int64{10000, 20000, 30000}) || w.UntilMS == nil || *w.UntilMS != 40000 {
		t.Errorf("before first: %+v %v", w, starts(w))
	}
	// Middle: previous, current, then up to pos+30 s.
	w = Cut(lines, 55_000, opt)
	if w.FromMS != 50000 || !reflect.DeepEqual(starts(w), []int64{40000, 50000, 60000, 70000, 80000}) || *w.UntilMS != 90000 {
		t.Errorf("middle: %+v %v", w, starts(w))
	}
	// After the last line: nothing remains.
	w = Cut(lines, 500_000, opt)
	if w.FromMS != 200000 || !reflect.DeepEqual(starts(w), []int64{190000, 200000}) || w.UntilMS != nil {
		t.Errorf("after last: %+v %v", w, starts(w))
	}
	// Dense lines: capped, UntilMS is the first dropped line.
	dense := seq(100, 500)
	w = Cut(dense, 5_000, opt)
	if len(w.Lines) != 10 || w.UntilMS == nil || *w.UntilMS != w.Lines[9].StartMS+500 {
		t.Errorf("dense: %v until %v", starts(w), w.UntilMS)
	}
	// The next line is included even when it is far ahead.
	sparse := []Line{{1000, "a"}, {120_000, "b"}, {130_000, "c"}}
	w = Cut(sparse, 2000, opt)
	if !reflect.DeepEqual(starts(w), []int64{1000, 120000}) || *w.UntilMS != 130000 {
		t.Errorf("far next: %v until %v", starts(w), w.UntilMS)
	}
	// Rune cut.
	long := []Line{{0, strings.Repeat("ế", 300)}}
	w = Cut(long, 0, WindowOptions{MaxLines: 3, MaxRunes: 200})
	if got := []rune(w.Lines[0].Text); len(got) != 200 {
		t.Errorf("rune cut = %d", len(got))
	}
	// Empty input.
	if w := Cut(nil, 1000, opt); len(w.Lines) != 0 || w.UntilMS != nil {
		t.Errorf("empty: %+v", w)
	}
}

func FuzzParseCut(f *testing.F) {
	f.Add("[00:01.00][00:30.00] a\n[offset:+100]\n[00:02] b", int64(1500))
	f.Add("[99:99] x\n[1:2:3] y", int64(0))
	f.Fuzz(func(t *testing.T, text string, pos int64) {
		lines := Parse(text)
		if !sort.SliceIsSorted(lines, func(a, b int) bool { return lines[a].StartMS < lines[b].StartMS }) {
			t.Fatal("unsorted")
		}
		w := Cut(lines, pos, WindowOptions{Ahead: 30 * time.Second, MaxLines: 10, MaxRunes: 200})
		if len(w.Lines) > 10 {
			t.Fatalf("%d lines", len(w.Lines))
		}
		if !sort.SliceIsSorted(w.Lines, func(a, b int) bool { return w.Lines[a].StartMS < w.Lines[b].StartMS }) {
			t.Fatal("window unsorted")
		}
	})
}
