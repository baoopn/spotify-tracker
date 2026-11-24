package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

const testKey = "sk-ant-test-not-a-real-key"

type fakeAPI struct {
	status  int
	header  http.Header
	body    string
	request map[string]any
	headers http.Header
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &f.request)
	f.headers = r.Header.Clone()
	for k, v := range f.header {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.status)
	_, _ = io.WriteString(w, f.body)
}

func message(stop, text string) string {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": stop, "stop_sequence": nil,
		"stop_details": map[string]any{"type": "refusal", "category": "cyber", "explanation": "x"},
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 40,
			"cache_creation_input_tokens": 5, "cache_read_input_tokens": 900},
	})
	return string(b)
}

func errBody(typ, code string) string {
	e := map[string]any{"type": typ, "message": "boom " + testKey}
	if code != "" {
		e["details"] = map[string]any{"error_code": code}
	}
	b, _ := json.Marshal(map[string]any{"type": "error", "error": e})
	return string(b)
}

func newTestClaude(t *testing.T, api *fakeAPI) *Claude {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return NewClaude(testKey, "claude-opus-5-5", "low", option.WithBaseURL(srv.URL))
}

var syncedReq = Request{TrackID: "t1", Title: "Song", Artist: "A", Kind: domain.TranslationSynced,
	Lines: []string{"một", "", "hai"}}

func TestClaudeRequest(t *testing.T) {
	api := &fakeAPI{status: 200, body: message("end_turn",
		`{"source_language":"vi","en":["one","two"],"vi":null}`)}
	res, err := newTestClaude(t, api).Translate(context.Background(), syncedReq)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.EN, "|") != "one||two" || res.VI != nil || res.Model != "claude-opus-5-5" ||
		res.InputTokens != 1005 || res.OutputTokens != 40 {
		t.Errorf("result = %+v", res)
	}
	req := api.request
	if req["model"] != "claude-opus-5-5" || req["thinking"] != nil || req["tool_choice"] != nil ||
		req["fallbacks"] != "default" || req["temperature"] != nil {
		t.Errorf("request = %v", req)
	}
	oc, _ := req["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if oc["effort"] != "low" || format["type"] != "json_schema" || format["schema"] == nil {
		t.Errorf("output_config = %v", oc)
	}
	sys, _ := req["system"].([]any)
	first, _ := sys[0].(map[string]any)
	if len(sys) != 1 || first["text"] != systemPrompt || first["cache_control"] == nil {
		t.Errorf("system = %v", sys)
	}
	msgs, _ := req["messages"].([]any)
	last, _ := msgs[len(msgs)-1].(map[string]any)
	if len(msgs) != 1 || last["role"] != "user" {
		t.Errorf("messages = %v", msgs)
	}
	if !strings.Contains(api.headers.Get("anthropic-beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("anthropic-beta = %q", api.headers.Get("anthropic-beta"))
	}
}

func TestClaudeErrors(t *testing.T) {
	isAPI := func(check func(*APIError) bool) func(error) bool {
		return func(err error) bool {
			var ae *APIError
			return errors.As(err, &ae) && check(ae)
		}
	}
	cases := map[string]struct {
		api  *fakeAPI
		want func(error) bool
	}{
		"refusal": {&fakeAPI{status: 200, body: message("refusal", "")}, func(err error) bool {
			var re *RefusalError
			return errors.As(err, &re) && re.Category == "cyber"
		}},
		"max tokens": {&fakeAPI{status: 200, body: message("max_tokens", "{")},
			func(err error) bool { return errors.Is(err, ErrTruncated) }},
		"bad output": {&fakeAPI{status: 200, body: message("end_turn", `{"source_language":"vi","en":["x"],"vi":null}`)},
			func(err error) bool { var ie *InvalidOutputError; return errors.As(err, &ie) }},
		"rate limit": {&fakeAPI{status: 429, header: http.Header{"Retry-After": {"30"}}, body: errBody("rate_limit_error", "")},
			isAPI(func(e *APIError) bool { return e.RateLimited() && e.RetryAfter.Seconds() == 30 })},
		"spend limit": {&fakeAPI{status: 429, body: errBody("rate_limit_error", "enforced_spend_limit_reached")},
			isAPI(func(e *APIError) bool { return e.SpendLimit() && !e.RateLimited() })},
		"auth":      {&fakeAPI{status: 401, body: errBody("authentication_error", "")}, isAPI((*APIError).Auth)},
		"permanent": {&fakeAPI{status: 400, body: errBody("invalid_request_error", "")}, isAPI((*APIError).Permanent)},
		"overloaded": {&fakeAPI{status: 529, body: errBody("overloaded_error", "")},
			isAPI(func(e *APIError) bool {
				return !e.Permanent() && !e.RateLimited() && !e.Auth() && e.Type == "overloaded_error"
			})},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newTestClaude(t, c.api).Translate(context.Background(), syncedReq)
			if err == nil || !c.want(err) {
				t.Errorf("err = %v (%T)", err, err)
			}
			if err != nil && strings.Contains(err.Error(), testKey) {
				t.Error("error leaks the key or body")
			}
		})
	}
}

// Neither logs nor errors carry the key, even through the worker.
func TestNoKeyInLogs(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	api := &fakeAPI{status: 500, body: errBody("api_error", "")}
	st := newFakeStore(cand("t1", "[00:01.00] một"))
	w := testWorker(st, newTestClaude(t, api), log)
	w.RunOnce(context.Background())
	if len(st.deferred) != 1 {
		t.Fatalf("deferred = %+v", st.deferred)
	}
	if strings.Contains(logs.String(), testKey) || strings.Contains(st.deferred[0].msg, testKey) {
		t.Error("key leaked")
	}
	if strings.Contains(st.deferred[0].msg, "boom") {
		t.Errorf("stored error holds the response body: %q", st.deferred[0].msg)
	}
}
