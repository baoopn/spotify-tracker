package lrc

import (
	"reflect"
	"sort"
	"strings"
	"testing"
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

func TestCutRunes(t *testing.T) {
	long := strings.Repeat("ế", 300)
	for _, c := range []struct {
		in   string
		n    int
		want int
	}{{long, 200, 200}, {long, 300, 300}, {long, 0, 300}, {"abc", 5, 3}, {"", 3, 0}} {
		if got := len([]rune(CutRunes(c.in, c.n))); got != c.want {
			t.Errorf("CutRunes(%d runes, %d) = %d runes, want %d", len([]rune(c.in)), c.n, got, c.want)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add("[00:01.00][00:30.00] a" + string(rune(10)) + "[offset:+100]" + string(rune(10)) + "[00:02] b")
	f.Add("[99:99] x" + string(rune(10)) + "[1:2:3] y")
	f.Fuzz(func(t *testing.T, text string) {
		lines := Parse(text)
		if !sort.SliceIsSorted(lines, func(a, b int) bool { return lines[a].StartMS < lines[b].StartMS }) {
			t.Fatal("unsorted")
		}
		for _, l := range lines {
			if l.StartMS < 0 {
				t.Fatalf("negative start %d", l.StartMS)
			}
		}
	})
}
