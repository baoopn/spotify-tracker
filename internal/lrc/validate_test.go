package lrc

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	in := "\uFEFF\r\n[00:01.00] a  \r\n[00:02.00] b\t\r[00:03.00] c\x00\n\n"
	if got, want := Normalize(in), "[00:01.00] a\n[00:02.00] b\n[00:03.00] c"; got != want {
		t.Errorf("Normalize = %q, want %q", got, want)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		dur  int64
		want []LineError
	}{
		{"ok", "[ar:Artist]\n[00:01.00] a\n\n[00:02.50] b\n[00:03.00]", 200_000, nil},
		{"repeated chorus not ordered", "[00:10.00] a\n[00:05.00][01:00.00] chorus\n[00:20.00] b", 0, nil},
		{"bad seconds", "[00:01.00] a\n[00:75.00] b", 0,
			[]LineError{{2, "invalid timestamp [00:75.00]: seconds must be below 60 and minutes at most 999"}}},
		{"bad format", "[00:01.00] a\n[1.5] b\n[00:02.1234] c", 0, []LineError{
			{2, "invalid timestamp [1.5]: use [mm:ss.xx]"}, {3, "invalid timestamp [00:02.1234]: use [mm:ss.xx]"}}},
		{"untimed", "[00:01.00] a\nplain words\n[Chorus]", 0, []LineError{
			{2, "no timestamp: start the line with [mm:ss.xx], or remove it"},
			{3, "no timestamp: start the line with [mm:ss.xx], or remove it"}}},
		{"out of order", "[00:05.00] a\n[00:03.00] b\n[00:06.00] c", 0,
			[]LineError{{2, "out of order: 0:03.00 comes before 0:05.00 on line 1"}}},
		{"past the end", "[00:01.00] a\n[03:30.00] b", 200_000,
			[]LineError{{2, "3:30.00 is past the end of the song (3:20.00)"}}},
		{"within slack", "[03:24.00] a", 200_000, nil},
		{"offset shifts before the check", "[offset:+20000]\n[03:35.00] a", 200_000, nil},
		{"no timed lines", "[ar:x]", 0, []LineError{{0, "no timed lines: synced lyrics need [mm:ss.xx] timestamps"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Validate(c.in, c.dur, 5000); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Validate = %#v, want %#v", got, c.want)
			}
		})
	}
	// Valid text parses to the same lines Parse sees.
	if errs := Validate("[00:01.00] a\n[00:02.00] b", 0, 0); errs != nil || len(Parse("[00:01.00] a\n[00:02.00] b")) != 2 {
		t.Error("valid text")
	}
	// Errors are capped.
	if got := Validate(strings.Repeat("x\n", 200), 0, 0); len(got) != MaxErrors {
		t.Errorf("errors = %d, want %d", len(got), MaxErrors)
	}
	if got := Validate(strings.Repeat("[00:01.00] a\n", MaxLines+1), 0, 0); len(got) != 1 || got[0].Line != 0 {
		t.Errorf("too many lines = %v", got)
	}
}

func TestPlain(t *testing.T) {
	in := "[ar:Artist]\n[offset:+100]\n[00:01.00] one <00:01.50>two\n[00:02.00]\n[00:02.50] ♪\n" +
		"[00:03.00][00:30.00] chorus\n[00:04.00] [Instrumental]\n[00:05.00] last"
	if got, want := Plain(in), "one two\n\nchorus\n\nlast"; got != want {
		t.Errorf("Plain = %q, want %q", got, want)
	}
	if got := Plain("[00:01.00] ♪\n[00:02.00]"); got != "" {
		t.Errorf("markers only = %q", got)
	}
}
