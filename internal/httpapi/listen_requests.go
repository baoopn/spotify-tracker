package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/logging"
)

// RequestSubmitter stores "Request an invite" submissions (implemented by
// *listen.Requests).
type RequestSubmitter interface {
	Submit(ctx context.Context, in listen.RequestInput) error
}

type okBody struct {
	OK bool `json:"ok"`
}

func accepted(w http.ResponseWriter) { writeJSON(w, http.StatusAccepted, okBody{OK: true}) }

// remoteIP is the client IP from the trusted-proxy rules (for Turnstile
// only; it is never stored).
func remoteIP(r *http.Request) string {
	if ip := middleware.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// POST /api/v1/listen/requests — public "Request an invite": {name, email,
// message?, website?, turnstile_token?}. Valid input always answers 202 {"ok":true}, whatever
// is known about the email, so it reveals nothing. website is a honeypot.
func (h *Handler) listenRequest(w http.ResponseWriter, r *http.Request) {
	o := h.opts.Listen
	if o.Requests == nil {
		writeError(w, http.StatusServiceUnavailable, "listen_unavailable", "invite requests are not available")
		return
	}
	var in struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Message string `json:"message"`
		Website string `json:"website"`
		// TurnstileToken is the Cloudflare Turnstile response (when on).
		TurnstileToken string `json:"turnstile_token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if listen.CleanText(in.Website, false) != "" {
		accepted(w) // a bot filled the hidden field: store nothing
		return
	}
	name, message := listen.CleanText(in.Name, false), listen.CleanText(in.Message, true)
	email, ok := listen.NormalizeEmail(listen.CleanText(in.Email, false))
	fields := map[string]string{}
	if n := utf8.RuneCountInString(name); n < 1 || n > listen.RequestNameMax {
		fields["name"] = "must be 1 to " + strconv.Itoa(listen.RequestNameMax) + " characters"
	}
	if !ok {
		fields["email"] = "must be a valid email address"
	}
	if utf8.RuneCountInString(message) > listen.RequestMessageMax {
		fields["message"] = "must be at most " + strconv.Itoa(listen.RequestMessageMax) + " characters"
	}
	token := strings.TrimSpace(in.TurnstileToken)
	if o.TurnstileSiteKey != "" && token == "" {
		fields["turnstile"] = "Complete the check"
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	ip, _ := clientIPKey(r)
	hash := o.ClientHash
	if hash == nil {
		hash = func(ip string) string { return analytics.HashIP("", "listen-request", ip) }
	}
	err := o.Requests.Submit(r.Context(), listen.RequestInput{Name: name, Email: email, Message: message,
		Country: analytics.Country(r), ClientHash: hash(ip), TurnstileToken: token, RemoteIP: remoteIP(r)})
	var limited listen.ErrRateLimited
	switch {
	case errors.Is(err, listen.ErrTurnstileFailed):
		writeValidation(w, map[string]string{"turnstile": "The check failed. Try again."})
	case errors.Is(err, listen.ErrTurnstileUnavailable):
		h.log.WarnContext(r.Context(), "turnstile check unavailable", logging.Err(err))
		writeError(w, http.StatusServiceUnavailable, "turnstile_unavailable", "the check could not be verified; try again soon")
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(limited.RetryAfter.Seconds()+0.999))))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; try again later")
	case err != nil:
		h.internalError(w, r, err)
	default:
		accepted(w)
	}
}
