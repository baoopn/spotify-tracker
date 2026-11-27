package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/baoopn/spotify-tracker/internal/analytics"
	"github.com/baoopn/spotify-tracker/internal/listen"
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

// POST /api/v1/listen/requests — public "Request an invite": {name, email,
// message?, website?}. Valid input always answers 202 {"ok":true}, whatever
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
		Country: analytics.Country(r), ClientHash: hash(ip)})
	var limited listen.ErrRateLimited
	switch {
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(limited.RetryAfter.Seconds()+0.999))))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; try again later")
	case err != nil:
		h.internalError(w, r, err)
	default:
		accepted(w)
	}
}
