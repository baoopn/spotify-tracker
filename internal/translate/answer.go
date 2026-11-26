package translate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

var langCode = regexp.MustCompile(`^([a-z]{2,3}|und)$`)

// answer is the model's JSON for either schema.
type answer struct {
	Languages      []string        `json:"languages"`
	SourceLanguage string          `json:"source_language"`
	EN             json.RawMessage `json:"en"`
	VI             json.RawMessage `json:"vi"`
	// Title and Album are read on their own: a bad answer for a name never
	// fails the lyrics.
	Title json.RawMessage `json:"title"`
	Album json.RawMessage `json:"album"`
}

// nameAnswer is a translated name (title or album).
type nameAnswer struct {
	EN *string `json:"en"`
	VI *string `json:"vi"`
}

// parseName reads a translated name, tolerantly: a missing or malformed
// object is no translation. Each value is one line cut to maxContextRunes;
// an empty one, or one equal to the original (case-insensitive), is "".
// An empty original (no album) gives no translation.
func parseName(raw json.RawMessage, original string) (en, vi string) {
	original = cutRunes(oneLine(original), maxContextRunes)
	if original == "" || isNull(raw) {
		return "", ""
	}
	var n nameAnswer
	if err := json.Unmarshal(raw, &n); err != nil {
		return "", ""
	}
	clean := func(v *string) string {
		if v == nil {
			return ""
		}
		s := cutRunes(oneLine(*v), maxContextRunes)
		if strings.EqualFold(s, original) {
			return ""
		}
		return s
	}
	return clean(n.EN), clean(n.VI)
}

// parseAnswer validates the model's JSON and fills a Result (without model
// and token fields). idx holds the indexes of the sent lines in r.Lines
// (synced only). A line that needs no translation is "".
func parseAnswer(r Request, raw string, idx []int) (Result, error) {
	var a answer
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return Result{}, &InvalidOutputError{Reason: "not JSON"}
	}
	lang := strings.ToLower(strings.TrimSpace(a.SourceLanguage))
	if !langCode.MatchString(lang) {
		return Result{}, &InvalidOutputError{Reason: "bad source_language"}
	}
	langs, err := languages(lang, a.Languages)
	if err != nil {
		return Result{}, err
	}
	// The main language is the first real one ("und" is dropped when
	// other languages are listed).
	out := Result{SourceLanguage: langs[0], Languages: langs}
	out.TitleEN, out.TitleVI = parseName(a.Title, r.Title)
	out.AlbumEN, out.AlbumVI = parseName(a.Album, r.Song.Album)
	for _, t := range []struct {
		code string
		raw  json.RawMessage
		dst  *[]string
		txt  *string
	}{{"en", a.EN, &out.EN, &out.ENText}, {"vi", a.VI, &out.VI, &out.VIText}} {
		if len(langs) == 1 && langs[0] == t.code {
			continue // all in this language: no translation, whatever was sent
		}
		if isNull(t.raw) {
			// The prompt asks for every target that is not the song's only
			// language. A null one anyway is stored as none (as before) and
			// reported, not retried: a retry costs credit and may say the same.
			out.MixedNull = append(out.MixedNull, t.code)
			continue
		}
		if r.Kind == domain.TranslationSynced {
			lines, kept, err := numberedLines(t.code, t.raw, r.Lines, idx)
			if err != nil {
				return Result{}, err
			}
			if kept {
				*t.dst = expand(lines, idx, len(r.Lines))
			} else {
				out.noneLeft(t.code)
			}
			continue
		}
		var text string
		if err := json.Unmarshal(t.raw, &text); err != nil {
			return Result{}, &InvalidOutputError{Reason: t.code + " is not text"}
		}
		if text = plainTranslation(text, r.Plain); text != "" {
			*t.txt = cutRunes(text, maxPlainRunes)
		} else {
			out.noneLeft(t.code)
		}
	}
	return out, nil
}

// noneLeft records a target whose every line came back empty (each was
// already in that language): it is stored as none, like null. Lyrics with no
// words ("und") need no translation, so only a song with real languages
// reports it.
func (r *Result) noneLeft(code string) {
	if len(r.Languages) != 1 || r.Languages[0] != "und" {
		r.MixedNull = append(r.MixedNull, code)
	}
}

// lineAnswer is one translated numbered line of a synced answer.
type lineAnswer struct {
	N    *int    `json:"n"`
	Text *string `json:"text"`
}

// numberedLines reads a synced target: items for only the lines that need a
// translation, each by its 1-based number in the user message. It returns
// one entry per sent line ("" for a line left out, empty, or copied) and
// whether any line holds a translation. A number out of range or listed
// twice fails the answer: the numbering went wrong, so a line could show
// another line's translation.
func numberedLines(code string, raw json.RawMessage, all []string, idx []int) ([]string, bool, error) {
	var items []lineAnswer
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false, &InvalidOutputError{Reason: code + " is not a list of numbered lines"}
	}
	lines := make([]string, len(idx))
	seen := make([]bool, len(idx))
	kept := false
	for _, it := range items {
		if it.N == nil || it.Text == nil {
			return nil, false, &InvalidOutputError{Reason: code + " has an item without n or text"}
		}
		n := *it.N
		if n < 1 || n > len(idx) {
			return nil, false, &InvalidOutputError{Reason: fmt.Sprintf("%s has line %d, want 1 to %d", code, n, len(idx))}
		}
		if seen[n-1] {
			return nil, false, &InvalidOutputError{Reason: fmt.Sprintf("%s has line %d twice", code, n)}
		}
		seen[n-1] = true
		// One line each: the admin API joins lines with newlines.
		lines[n-1] = blankCopy(cutRunes(oneLine(*it.Text), maxLineRunes), all[idx[n-1]])
		kept = kept || lines[n-1] != ""
	}
	return lines, kept, nil
}

// blankCopy is tr, or "" when the model copied the original line instead of
// leaving it empty (the prompt asks for "" when a line needs no translation).
func blankCopy(tr, original string) string {
	if strings.EqualFold(oneLine(tr), oneLine(original)) {
		return ""
	}
	return tr
}

// plainTranslation cleans a plain translation: each line trimmed, with the
// same line breaks as the text the model saw, so line N stays line N
// (leading and trailing empty lines too). With the same number of lines, a
// line copied from the original is left empty. Extra empty lines at the end
// are dropped. It is "" when no line holds a translation.
func plainTranslation(text, plain string) string {
	orig := strings.Split(strings.TrimSpace(plain), "\n")
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	for len(lines) > len(orig) && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == len(orig) {
		for i := range lines {
			lines[i] = blankCopy(lines[i], orig[i])
		}
	}
	out := strings.Join(lines, "\n")
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return out
}

// languages normalises the model's language list: lower case, valid codes,
// no repeats, main (the source language) first, and "und" only when there
// is nothing else. An empty list (an older answer shape) is just the main
// language.
func languages(main string, got []string) ([]string, error) {
	out := []string{main}
	seen := map[string]bool{main: true}
	for _, l := range got {
		l = strings.ToLower(strings.TrimSpace(l))
		if !langCode.MatchString(l) {
			return nil, &InvalidOutputError{Reason: "bad languages"}
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	if len(out) > 1 {
		kept := out[:0]
		for _, l := range out {
			if l != "und" {
				kept = append(kept, l)
			}
		}
		out = kept
	}
	return out, nil
}

func isNull(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null"
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
