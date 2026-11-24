package translate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

func TestCompactExpand(t *testing.T) {
	lines := []string{"một", "", "  hai ", "", "một"}
	texts, idx := compact(lines)
	if strings.Join(texts, "|") != "một|hai|một" || len(idx) != 3 || idx[1] != 2 || idx[2] != 4 {
		t.Fatalf("compact = %q %v", texts, idx)
	}
	got := expand([]string{"one", "two", "one"}, idx, len(lines))
	if strings.Join(got, "|") != "one||two||one" {
		t.Errorf("expand = %q", got)
	}
}

func TestUserMessage(t *testing.T) {
	r := Request{Title: "Chờ\nEm", Artist: "A", Kind: domain.TranslationSynced}
	msg := userMessage(r, []string{"dòng một", "dòng\nhai"})
	for _, want := range []string{"Title: Chờ Em\n", "Format: synced, 2 numbered lines", "1| dòng một\n2| dòng hai\n", "<lyrics>", "</lyrics>"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	plain := userMessage(Request{Title: "T", Artist: "A", Kind: domain.TranslationPlain, Plain: "  a\n\nb  "}, nil)
	if !strings.Contains(plain, "Format: plain text\n\n<lyrics>\na\n\nb\n</lyrics>") {
		t.Errorf("plain message:\n%s", plain)
	}
}

// The schemas are valid, closed, and avoid keywords structured outputs
// does not support; the system prompt has no variable parts.
func TestSchemasAndPrompt(t *testing.T) {
	for name, s := range map[string]json.RawMessage{"synced": syncedSchema, "plain": plainSchema} {
		var v map[string]any
		if err := json.Unmarshal(s, &v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties must be false", name)
		}
		for _, bad := range []string{"minItems", "maxItems", "maxLength", "minLength"} {
			if strings.Contains(string(s), bad) {
				t.Errorf("%s uses %s", name, bad)
			}
		}
	}
	if strings.ContainsAny(systemPrompt, "%{}") {
		t.Error("system prompt must be a fixed text")
	}
}

func TestParseAnswer(t *testing.T) {
	synced := Request{Kind: domain.TranslationSynced, Lines: []string{"một", "", "hai"}}
	_, idx := compact(synced.Lines)
	ok, err := parseAnswer(synced, `{"source_language":"VI","en":["one","two\nlines"],"vi":["ignored","x"]}`, idx)
	if err != nil {
		t.Fatal(err)
	}
	if ok.SourceLanguage != "vi" || ok.VI != nil || strings.Join(ok.EN, "|") != "one||two lines" {
		t.Errorf("synced = %+v", ok)
	}

	plain := Request{Kind: domain.TranslationPlain, Plain: "hello"}
	p, err := parseAnswer(plain, `{"source_language":"en","en":null,"vi":"  xin chào  "}`, nil)
	if err != nil || p.ENText != "" || p.VIText != "xin chào" {
		t.Errorf("plain = %+v, %v", p, err)
	}

	for name, raw := range map[string]string{
		"not json":     `nope`,
		"bad language": `{"source_language":"Vietnamese","en":["a","b"],"vi":null}`,
		"wrong count":  `{"source_language":"vi","en":["a"],"vi":null}`,
		"missing en":   `{"source_language":"ko","en":null,"vi":["a","b"]}`,
		"not lines":    `{"source_language":"vi","en":"a","vi":null}`,
	} {
		var ie *InvalidOutputError
		if _, err := parseAnswer(synced, raw, idx); !errors.As(err, &ie) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := parseAnswer(plain, `{"source_language":"vi","en":"  ","vi":null}`, nil); err == nil {
		t.Error("empty plain translation must fail")
	}
}
