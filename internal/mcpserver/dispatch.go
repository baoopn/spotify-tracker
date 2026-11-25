package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolTimeout bounds one in-process API call (the /mcp route times out
// after 30 s).
const toolTimeout = 25 * time.Second

// addTools registers defs on s. Each tool runs a request (GET unless the
// def says otherwise) for prefix + def.path through target and returns its
// JSON.
//
// Tools use the low-level Server.AddTool with their own argument handling:
// the generic mcp.AddTool panics (in a goroutine, killing the process) when
// a client sends "arguments": null to a tool whose schema has defaults.
func addTools(s *mcp.Server, defs []toolDef, target http.Handler, prefix string, log *slog.Logger, public bool) {
	for _, d := range defs {
		ann := &mcp.ToolAnnotations{ReadOnlyHint: d.method == "", IdempotentHint: true, Title: d.title}
		if d.method != "" {
			ann.DestructiveHint = new(d.destructive)
		}
		if public {
			ann.OpenWorldHint = new(false)
		}
		schema := d.schema()
		resolved, err := schema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
		if err != nil {
			panic(fmt.Sprintf("mcpserver: tool %s: invalid input schema: %v", d.name, err))
		}
		tool := &mcp.Tool{Name: d.name, Title: d.title, Description: d.desc, InputSchema: schema, Annotations: ann}
		s.AddTool(tool, d.handler(resolved, target, prefix, log))
	}
}

// arguments decodes, defaults and validates the raw tool arguments (absent
// or null means no arguments). An optional argument set to null counts as
// unset (LLM clients often send them); a null required argument is left in
// place so that validation reports it.
func arguments(raw json.RawMessage, resolved *jsonschema.Resolved) (map[string]any, error) {
	args := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, errors.New("arguments must be a JSON object")
		}
		if args == nil {
			args = map[string]any{}
		}
	}
	required := resolved.Schema().Required
	for k, v := range args {
		if v == nil && !slices.Contains(required, k) {
			delete(args, k)
		}
	}
	var instance any = args
	if err := resolved.ApplyDefaults(&instance); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if err := resolved.Validate(&instance); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return args, nil
}

// schema is the tool's JSON input schema.
func (d toolDef) schema() *jsonschema.Schema {
	s := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}
	for _, p := range d.params {
		ps := &jsonschema.Schema{Type: "string", Description: p.desc}
		if p.boolean {
			ps.Type = "boolean"
		}
		if p.integer {
			lo, hi := float64(p.min), float64(p.max)
			ps.Type, ps.Minimum, ps.Maximum = "integer", &lo, &hi
		}
		for _, v := range p.enum {
			ps.Enum = append(ps.Enum, v)
		}
		if p.maxLen > 0 {
			ps.MaxLength = new(p.maxLen)
		}
		if p.required && !p.integer {
			ps.MinLength = new(1)
		}
		if p.def != nil {
			ps.Default, _ = json.Marshal(p.def)
		}
		s.Properties[p.name] = ps
		if p.required {
			s.Required = append(s.Required, p.name)
		}
	}
	return s
}

// target builds the request URL from validated arguments: path params are
// escaped as one segment each, the others go into the query string (empty
// values are left out, so the API applies its defaults).
func (d toolDef) target(prefix string, args map[string]any) (string, error) {
	path := d.path
	q := url.Values{}
	for _, p := range d.params {
		if p.inBody {
			continue
		}
		v, ok := args[p.name]
		if !ok || v == nil {
			if p.required {
				return "", fmt.Errorf("%s is required", p.name)
			}
			continue
		}
		s, err := argString(v)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p.name, err)
		}
		switch {
		case p.inPath:
			if s == "" {
				return "", fmt.Errorf("%s is required", p.name)
			}
			path = strings.Replace(path, "{"+p.name+"}", url.PathEscape(s), 1)
		case s != "":
			q.Set(p.name, s)
		}
	}
	u := prefix + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u, nil
}

// argString formats a decoded JSON argument for a URL.
func argString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case json.Number:
		return x.String(), nil
	case bool:
		return strconv.FormatBool(x), nil
	}
	return "", errors.New("must be a string or a number")
}

func (d toolDef) handler(resolved *jsonschema.Resolved, target http.Handler, prefix string, log *slog.Logger) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var raw json.RawMessage
		if req != nil && req.Params != nil {
			raw = req.Params.Arguments
		}
		args, err := arguments(raw, resolved)
		if err != nil {
			return toolError(err.Error()), nil
		}
		u, err := d.target(prefix, args)
		if err != nil {
			return toolError(err.Error()), nil
		}
		return callBody(ctx, target, d.method, u, d.body(args), d.name, log), nil
	}
}

// call runs method (GET when empty) u through h and turns the response into a tool result: the
// JSON body on 2xx, the API's error message on 4xx, "internal error" else.
//
// Every panic is recovered, http.ErrAbortHandler included: tool handlers
// run on an SDK goroutine, not the net/http one, so a re-raised panic would
// crash the process.
func call(ctx context.Context, h http.Handler, method, u, tool string, log *slog.Logger) *mcp.CallToolResult {
	return callBody(ctx, h, method, u, nil, tool, log)
}

// callBody is call with a JSON request body (nil = none).
func callBody(ctx context.Context, h http.Handler, method, u string, body []byte, tool string, log *slog.Logger) (res *mcp.CallToolResult) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			log.ErrorContext(ctx, "mcp tool panic", slog.String("tool", tool), slog.Any("panic", p))
			res = toolError("internal error")
		}
	}()

	// ctx comes from the outer POST /mcp request and carries its chi route
	// context. The inner handlers are chi routers too: given that context
	// they would route the outer method and path (405 for every tool) and
	// append their patterns to the outer route label. Start a clean one.
	ctx = context.WithValue(ctx, chi.RouteCtxKey, chi.NewRouteContext())
	if method == "" {
		method = http.MethodGet
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return toolError("invalid arguments")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := &recorder{header: http.Header{}}
	h.ServeHTTP(rec, req)

	status := rec.code()
	out := bytes.TrimSpace(rec.buf.Bytes())
	switch {
	case status >= 200 && status < 300:
		var v any
		if err := json.Unmarshal(out, &v); err != nil {
			log.ErrorContext(ctx, "mcp tool: invalid JSON from API", slog.String("tool", tool), slog.Int("status", status))
			return toolError("internal error")
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(out)}}, StructuredContent: v}
	case status >= 400 && status < 500:
		return toolError(apiErrorMessage(out, status))
	default:
		log.WarnContext(ctx, "mcp tool failed", slog.String("tool", tool), slog.Int("status", status))
		return toolError("internal error")
	}
}

func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

// apiErrorMessage reads {"error":{"message":...,"fields":{...}}} as written
// by the public and admin APIs, falling back to the status text.
func apiErrorMessage(body []byte, status int) string {
	var e struct {
		Error struct {
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
			Lines   []struct {
				Line    int    `json:"line"`
				Message string `json:"message"`
			} `json:"lines"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil || e.Error.Message == "" {
		return http.StatusText(status)
	}
	msg := e.Error.Message
	for _, k := range slices.Sorted(maps.Keys(e.Error.Fields)) {
		msg += "; " + k + ": " + e.Error.Fields[k]
	}
	for _, l := range e.Error.Lines {
		if l.Line > 0 {
			msg += fmt.Sprintf("; line %d: %s", l.Line, l.Message)
		} else {
			msg += "; " + l.Message
		}
	}
	return msg
}

// recorder is a minimal in-memory http.ResponseWriter.
type recorder struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.buf.Write(p)
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}

func (r *recorder) code() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// body is the JSON request body of the tool's inBody arguments (as given,
// strings untrimmed), or nil when the tool has none.
func (d toolDef) body(args map[string]any) []byte {
	var m map[string]any
	for _, p := range d.params {
		if !p.inBody {
			continue
		}
		if m == nil {
			m = map[string]any{}
		}
		if v, ok := args[p.name]; ok && v != nil {
			m[p.name] = v
		}
	}
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}
