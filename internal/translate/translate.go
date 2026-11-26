// Package translate translates found lyrics into English and Vietnamese with
// the Claude API, in the background: a Worker picks lyrics with no current
// translation and stores the result. Synced lyrics are translated line by
// line, so each translated line lines up with its LRC line.
package translate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// Request is one song's lyrics to translate.
type Request struct {
	TrackID string
	Title   string
	Artist  string
	// Kind is domain.TranslationSynced (Lines) or domain.TranslationPlain (Plain).
	Kind  string
	Lines []string // parsed LRC lines in order; "" for empty lines
	Plain string
	// Song is what the database knows about the song (album, genres, ...),
	// sent with the lyrics; every field may be empty.
	Song domain.TranslationContext
}

// Result is a translation. For synced lyrics EN and VI have one entry per
// request line (nil = the song is already in that language); for plain
// lyrics ENText and VIText hold the text ("" = already in that language).
// A language is left empty only when Languages is just that language.
type Result struct {
	SourceLanguage string   // main language (ISO 639-1, or "und")
	Languages      []string // every language in the lyrics, main first
	// MixedNull lists the targets the model left null although Languages
	// has other languages too (stored as none; the worker logs a warning).
	MixedNull      []string
	EN, VI         []string
	ENText, VIText string
	// TitleEN, TitleVI, AlbumEN and AlbumVI are the song title and album
	// name in each target language; "" = none (kept as written, or no album).
	TitleEN, TitleVI string
	AlbumEN, AlbumVI string
	Model            string // the model that answered (a fallback shows here)
	InputTokens      int64
	OutputTokens     int64
}

// Translator translates one song (implemented by *Claude).
type Translator interface {
	Translate(ctx context.Context, r Request) (Result, error)
}

// APIError is a Claude API error, reduced to what the worker needs. It never
// carries the response body.
type APIError struct {
	Status     int
	Type       string // error type, e.g. "overloaded_error"
	Code       string // error.details.error_code, when present
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("claude: status %d", e.Status)
	if e.Type != "" {
		msg += " " + e.Type
	}
	if e.Code != "" {
		msg += " (" + e.Code + ")"
	}
	return msg
}

// SpendLimit reports that the account is out of credit or over its spend limit.
func (e *APIError) SpendLimit() bool {
	switch e.Code {
	case "enforced_spend_limit_reached", "contract_spend_limit_reached", "linked_organization_spend_limit_reached":
		return true
	}
	return e.Status == http.StatusPaymentRequired
}

// Auth reports a bad or unauthorized API key.
func (e *APIError) Auth() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// RateLimited reports a 429 that is not a spend limit.
func (e *APIError) RateLimited() bool {
	return e.Status == http.StatusTooManyRequests && !e.SpendLimit()
}

// Permanent reports a request the API will refuse again (retrying soon is useless).
func (e *APIError) Permanent() bool {
	switch e.Status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// RefusalError means the model declined to translate (stop_reason refusal).
type RefusalError struct{ Category string }

func (e *RefusalError) Error() string {
	if e.Category == "" {
		return "claude: refused"
	}
	return "claude: refused (" + e.Category + ")"
}

// ErrTruncated means the answer hit max_tokens.
var ErrTruncated = errors.New("claude: answer cut off at max_tokens")

// InvalidOutputError means the answer did not match what was asked (bad JSON,
// wrong line count, unknown language code).
type InvalidOutputError struct{ Reason string }

func (e *InvalidOutputError) Error() string { return "claude: invalid output: " + e.Reason }
