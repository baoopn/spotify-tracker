package listen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TurnstileVerifyURL is Cloudflare's Turnstile siteverify endpoint.
const TurnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

const turnstileTimeout = 5 * time.Second

var (
	// ErrTurnstileFailed means Cloudflare rejected the token (or it was made
	// for another site).
	ErrTurnstileFailed = errors.New("turnstile check failed")
	// ErrTurnstileUnavailable means siteverify could not answer.
	ErrTurnstileUnavailable = errors.New("turnstile siteverify unavailable")
)

// Turnstile verifies Cloudflare Turnstile tokens server-side. The secret
// and the tokens are never logged or put in errors.
type Turnstile struct {
	secret string
	// hostname, when set, must match the hostname in Cloudflare's reply (the
	// site the widget ran on).
	hostname string
	url      string
	client   *http.Client
}

// NewTurnstile builds a verifier for secret. hostname is the expected site
// host ("" skips the check, e.g. in development).
func NewTurnstile(secret, hostname string) *Turnstile {
	return &Turnstile{secret: secret, hostname: strings.ToLower(hostname), url: TurnstileVerifyURL,
		client: &http.Client{Timeout: turnstileTimeout}}
}

type siteverifyReply struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify checks token for the client at remoteIP. It returns
// ErrTurnstileFailed when Cloudflare says no (or the hostname differs), and
// an error wrapping ErrTurnstileUnavailable when Cloudflare cannot answer
// (network error, timeout, non-200 reply).
func (t *Turnstile) Verify(ctx context.Context, token, remoteIP string) error {
	form := url.Values{"secret": {t.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	ctx, cancel := context.WithTimeout(ctx, turnstileTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: bad request", ErrTurnstileUnavailable)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		// stripURL keeps the message short; the URL holds no secret.
		return fmt.Errorf("%w: %w", ErrTurnstileUnavailable, stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: siteverify answered %d", ErrTurnstileUnavailable, resp.StatusCode)
	}
	var reply siteverifyReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply); err != nil {
		return fmt.Errorf("%w: bad reply", ErrTurnstileUnavailable)
	}
	if !reply.Success {
		return ErrTurnstileFailed
	}
	if t.hostname != "" && !strings.EqualFold(reply.Hostname, t.hostname) {
		return ErrTurnstileFailed
	}
	return nil
}
