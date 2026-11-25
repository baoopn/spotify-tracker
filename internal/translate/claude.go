package translate

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// maxTokens bounds one answer, thinking included.
const maxTokens = 16000

// Claude translates with the Claude Messages API. The API key lives only in
// the SDK client; Claude has no field or String method that exposes it.
type Claude struct {
	client anthropic.Client
	model  string
	effort anthropic.BetaOutputConfigEffort
}

// NewClaude returns a translator using model at effort ("low" or "medium").
// The SDK does not retry: the worker owns retries, so each call counts once
// against the caps. opts are for tests (base URL, HTTP client).
func NewClaude(apiKey, model, effort string, opts ...option.RequestOption) *Claude {
	base := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(0),
		option.WithRequestTimeout(3 * time.Minute)}
	return &Claude{client: anthropic.NewClient(append(base, opts...)...), model: model,
		effort: anthropic.BetaOutputConfigEffort(effort)}
}

// Translate translates one song. Errors are *APIError, *RefusalError,
// ErrTruncated, *InvalidOutputError, or a network/context error; none
// carries the response body. When the API answered (also with a refusal, a
// cut-off or an unusable answer) the Result holds the billed tokens.
func (c *Claude) Translate(ctx context.Context, r Request) (Result, error) {
	var texts []string
	var idx []int
	schema := plainSchema
	if r.Kind == domain.TranslationSynced {
		texts, idx = compact(r.Lines)
		schema = syncedSchema
	}
	params := anthropic.BetaMessageNewParams{
		Model:     c.model,
		MaxTokens: maxTokens,
		// Thinking is left unset (adaptive): claude-sonnet-5-5 rejects
		// {type: "disabled"}, and the explicit effort (low; the API default
		// is high) keeps it short.
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: c.effort,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: schema},
		},
		// The fixed prompt goes first so it can be cached across songs.
		System: []anthropic.BetaTextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(userMessage(r, texts))),
		},
		// A refusal is answered again by a fallback model inside the same call.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	}
	msg, err := c.client.Beta.Messages.New(ctx, params)
	if err != nil {
		return Result{}, apiError(err)
	}
	u := msg.Usage
	billed := Result{Model: msg.Model, OutputTokens: u.OutputTokens,
		InputTokens: u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return billed, &RefusalError{Category: string(msg.StopDetails.Category)}
	case anthropic.BetaStopReasonMaxTokens:
		return billed, ErrTruncated
	}
	var text string
	for _, b := range msg.Content {
		if b.Type == "text" {
			text = b.Text
			break
		}
	}
	out, err := parseAnswer(r, text, idx)
	if err != nil {
		return billed, err
	}
	out.Model, out.InputTokens, out.OutputTokens = billed.Model, billed.InputTokens, billed.OutputTokens
	return out, nil
}

// apiError reduces an SDK error to an *APIError (status, type, error code,
// Retry-After), dropping the request URL and response body. Other errors
// (network, context) pass through.
func apiError(err error) error {
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		return err
	}
	out := &APIError{Status: ae.StatusCode, Type: string(ae.Type()), Code: ae.Details.ErrorCode}
	if ae.Response != nil {
		if s, e := strconv.Atoi(ae.Response.Header.Get("retry-after")); e == nil && s > 0 {
			out.RetryAfter = time.Duration(s) * time.Second
		}
	}
	return out
}
