package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/baoopn/spotify-tracker/internal/observability"
)

// Player control errors. They are returned wrapped; test with errors.Is.
var (
	// ErrNoActiveDevice means Spotify has no device to act on (404
	// NO_ACTIVE_DEVICE): open Spotify on a device, or pass a device id.
	ErrNoActiveDevice = errors.New("spotify: no active device")
	// ErrPremiumRequired means the command needs a Spotify Premium account
	// (403 PREMIUM_REQUIRED).
	ErrPremiumRequired = errors.New("spotify: premium required")
	// ErrRestricted means Spotify refused the command (403), for example a
	// restricted device, an action the current context disallows, or a
	// token without the needed scope.
	ErrRestricted = errors.New("spotify: player command refused")
	// ErrInsufficientScope means Spotify refused a player call because the
	// token lacks a scope (403 "Insufficient client scope"): re-authorize.
	ErrInsufficientScope = errors.New("spotify: token is missing a playback scope")
	// ErrInvalidArgument means a player call got an invalid argument; no
	// request was sent. The error is an *ArgError naming the field.
	ErrInvalidArgument = errors.New("spotify: invalid argument")
)

// ArgError is an invalid player call argument. Field is the JSON name the
// admin API uses for it (e.g. "volume_percent"). It matches
// ErrInvalidArgument with errors.Is.
type ArgError struct {
	Field string
	Msg   string
}

func (e *ArgError) Error() string { return "spotify: invalid argument: " + e.Field + ": " + e.Msg }

// Is makes errors.Is(err, ErrInvalidArgument) true.
func (e *ArgError) Is(target error) bool { return target == ErrInvalidArgument }

func argErr(field, format string, args ...any) error {
	return &ArgError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

// playerErrorBody is the Web API error object.
type playerErrorBody struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	} `json:"error"`
}

// call sends one Web API request for the player with the poller's token
// source, retrying once with a forced token refresh on 401. body (nil for
// none) is sent as JSON; out (nil to ignore) is decoded from a 200 answer.
// It returns the HTTP status: 200, 201, 202 or 204 on success.
func (p *Player) call(ctx context.Context, what, method, path string, q url.Values, body, out any) (int, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("spotify: encode %s: %w", what, err)
		}
		payload = b
	}
	target := p.apiBase + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := p.tokens.AccessToken(ctx, attempt > 0)
		if err != nil {
			return 0, err
		}
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rd)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := p.http.Do(req)
		if err != nil {
			return 0, fmt.Errorf("spotify: %s: %w", what, err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		observability.UpstreamRequests.WithLabelValues("spotify", strconv.Itoa(resp.StatusCode)).Inc()

		switch resp.StatusCode {
		case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
			if out != nil && resp.StatusCode == http.StatusOK && len(bytes.TrimSpace(raw)) > 0 {
				if err := json.Unmarshal(raw, out); err != nil {
					return 0, fmt.Errorf("spotify: decode %s: %w", what, err)
				}
			}
			return resp.StatusCode, nil
		case http.StatusUnauthorized:
			continue
		case http.StatusTooManyRequests:
			return 0, rateLimitFrom(&http.Response{Header: resp.Header, Body: io.NopCloser(bytes.NewReader(raw))})
		default:
			return 0, playerError(what, resp.StatusCode, raw)
		}
	}
	return 0, &StatusError{Code: http.StatusUnauthorized,
		msg: fmt.Sprintf("spotify: %s: unauthorized after token refresh", what)}
}

// playerError maps a failed player answer to the typed errors.
func playerError(what string, code int, raw []byte) error {
	var eb playerErrorBody
	_ = json.Unmarshal(raw, &eb)
	msg := strings.TrimSpace(eb.Error.Message)
	if msg == "" {
		msg = truncate(raw)
	}
	switch {
	case code == http.StatusNotFound && (eb.Error.Reason == "NO_ACTIVE_DEVICE" ||
		strings.Contains(strings.ToLower(msg), "no active device")):
		return fmt.Errorf("%w: %s", ErrNoActiveDevice, msg)
	case code == http.StatusNotFound:
		return fmt.Errorf("%w: %s: %s", ErrNotFound, what, msg)
	case code == http.StatusForbidden && strings.Contains(strings.ToLower(msg), "scope"):
		return fmt.Errorf("%w: %s", ErrInsufficientScope, msg)
	case code == http.StatusForbidden && eb.Error.Reason == "PREMIUM_REQUIRED":
		return fmt.Errorf("%w: %s", ErrPremiumRequired, msg)
	case code == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrRestricted, msg)
	}
	return &StatusError{Code: code, msg: fmt.Sprintf("spotify: %s failed (%d): %s", what, code, msg)}
}

// deviceQuery returns ?device_id= for an optional device id ("" = the
// active device), with the other query values in extra.
func deviceQuery(deviceID string, extra url.Values) (url.Values, error) {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	if deviceID != "" {
		if err := ValidateDeviceID(deviceID); err != nil {
			return nil, err
		}
		q.Set("device_id", deviceID)
	}
	return q, nil
}

// ValidateDeviceID checks a Spotify Connect device id: non-empty, at most
// 128 characters, no spaces or control characters.
func ValidateDeviceID(id string) error {
	if id == "" || len(id) > 128 || strings.ContainsFunc(id, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return argErr("device_id", "is not a valid device id")
	}
	return nil
}

// TrackURI returns the spotify:track: URI of a track id or URI, or false
// when it is neither.
func TrackURI(s string) (string, bool) {
	id := strings.TrimPrefix(s, "spotify:track:")
	if !trackIDPattern.MatchString(id) {
		return "", false
	}
	return "spotify:track:" + id, true
}

// validContextURI reports a playable context URI (album, playlist,
// artist or show).
func validContextURI(s string) bool {
	kind, id, ok := strings.Cut(strings.TrimPrefix(s, "spotify:"), ":")
	if !strings.HasPrefix(s, "spotify:") || !ok || !trackIDPattern.MatchString(id) {
		return false
	}
	switch kind {
	case "album", "playlist", "artist", "show":
		return true
	}
	return false
}

// Repeat modes for SetRepeat.
const (
	RepeatOff     = "off"
	RepeatTrack   = "track"
	RepeatContext = "context"
)

// validRepeat reports whether mode is a repeat mode.
func validRepeat(mode string) bool {
	return mode == RepeatOff || mode == RepeatTrack || mode == RepeatContext
}
