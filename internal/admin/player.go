package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// playerControl is the owner's Spotify player (implemented by
// *spotify.Player over the Manager's tokens).
type playerControl interface {
	PlaybackState(ctx context.Context) (*spotify.PlaybackState, error)
	Devices(ctx context.Context) ([]spotify.Device, error)
	PlayerQueue(ctx context.Context, limit int) (*spotify.PlayerQueue, error)
	SearchTracks(ctx context.Context, q string, limit int) ([]spotify.PlayerTrack, error)
	Play(ctx context.Context, o spotify.PlayOptions) error
	Pause(ctx context.Context, deviceID string) error
	Next(ctx context.Context, deviceID string) error
	Previous(ctx context.Context, deviceID string) error
	Seek(ctx context.Context, positionMS int64, deviceID string) error
	SetVolume(ctx context.Context, percent int, deviceID string) error
	SetShuffle(ctx context.Context, on bool, deviceID string) error
	SetRepeat(ctx context.Context, mode, deviceID string) error
	Transfer(ctx context.Context, deviceID string, play bool) error
	AddToQueue(ctx context.Context, track, deviceID string) error
}

// playerQueueShown is how many upcoming tracks GET /player returns.
const playerQueueShown = 20

// playerCallTimeout bounds the Spotify calls of one player request.
const playerCallTimeout = 10 * time.Second

// livePlayer is the shared Player built from the Manager on first use.
type livePlayer struct {
	once sync.Once
	p    *spotify.Player
}

// mountPlayer registers the player routes. They are left out of
// ReadOnlyHandler on purpose: only signed-in admins control playback.
func (a *API) mountPlayer(r chi.Router) {
	r.Get("/player", a.getPlayer)
	r.Get("/player/search", a.searchPlayer)
	r.Post("/player/play", a.playerAction("play", a.playerPlay))
	r.Post("/player/pause", a.playerAction("pause", deviceOnly(playerControl.Pause)))
	r.Post("/player/next", a.playerAction("next", deviceOnly(playerControl.Next)))
	r.Post("/player/previous", a.playerAction("previous", deviceOnly(playerControl.Previous)))
	r.Post("/player/seek", a.playerAction("seek", a.playerSeek))
	r.Post("/player/volume", a.playerAction("volume", a.playerVolume))
	r.Post("/player/shuffle", a.playerAction("shuffle", a.playerShuffle))
	r.Post("/player/repeat", a.playerAction("repeat", a.playerRepeat))
	r.Post("/player/transfer", a.playerAction("transfer", a.playerTransfer))
	r.Post("/player/queue", a.playerAction("queue", a.playerQueueAdd))
}

func (a *API) playerClient() playerControl {
	if a.player != nil {
		return a.player
	}
	a.live.once.Do(func() {
		a.live.p = spotify.NewPlayer(tokenFunc(func(ctx context.Context, force bool) (string, error) {
			tok, _, err := a.Spotify.AccessToken(ctx, force)
			return tok, err
		}), "", nil)
	})
	return a.live.p
}

// controlAccess reports whether the stored token may control playback.
// Unknown scopes (a token seeded from the environment) are given a try.
func (a *API) controlAccess(ctx context.Context) (connected, canControl bool, err error) {
	sp, err := a.spotifySettings(ctx)
	switch {
	case errors.Is(err, settings.ErrNotFound):
		return false, false, nil
	case err != nil:
		return false, false, err
	}
	connected = sp.RefreshToken != ""
	return connected, connected && (len(sp.Scopes) == 0 || spotify.HasScope(sp.Scopes, spotify.PlaybackControlScope)), nil
}

// ─── reads ────────────────────────────────────────────────────────────────────

// Parts of GET /player, chosen with ?parts= (default all). The SPA polls
// the state often and the devices and queue less often, to spare the
// Spotify rate limit the poller shares.
const (
	partState   = "state"
	partDevices = "devices"
	partQueue   = "queue"
)

func playerParts(raw string) (map[string]bool, bool) {
	if raw == "" {
		return map[string]bool{partState: true, partDevices: true, partQueue: true}, true
	}
	parts := map[string]bool{}
	for _, p := range strings.Split(raw, ",") {
		switch p = strings.TrimSpace(p); p {
		case partState, partDevices, partQueue:
			parts[p] = true
		default:
			return nil, false
		}
	}
	return parts, len(parts) > 0
}

// getPlayer serves GET /player[?parts=state,devices,queue]: connected and
// can_control, plus the parts asked for. The state is null when no device
// is active; the devices and queue are best effort (null when Spotify
// refuses them) and are left out when not asked for.
func (a *API) getPlayer(w http.ResponseWriter, r *http.Request) {
	parts, ok := playerParts(r.URL.Query().Get("parts"))
	if !ok {
		writeValidation(w, map[string]string{"parts": "must list state, devices and/or queue"})
		return
	}
	connected, canControl, err := a.controlAccess(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	v := map[string]any{"connected": connected, "can_control": canControl}
	for p := range parts {
		v[p] = nil
	}
	if !connected {
		writeJSON(w, http.StatusOK, v)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), playerCallTimeout)
	defer cancel()
	p := a.playerClient()

	var (
		wg                sync.WaitGroup
		state             *spotify.PlaybackState
		devices           []spotify.Device
		queue             *spotify.PlayerQueue
		stErr, dErr, qErr error
	)
	if parts[partState] {
		wg.Go(func() { state, stErr = p.PlaybackState(ctx) })
	}
	if parts[partDevices] {
		wg.Go(func() { devices, dErr = p.Devices(ctx) })
	}
	if parts[partQueue] {
		wg.Go(func() { queue, qErr = p.PlayerQueue(ctx, playerQueueShown) })
	}
	wg.Wait()

	// The state is the main part: its error fails the request. With only
	// the devices and queue asked for, a rate limit fails it too, so the
	// SPA backs off.
	var rl *spotify.RateLimitError
	switch {
	case stErr != nil:
		a.playerFail(w, r, stErr)
		return
	case errors.As(dErr, &rl):
		a.playerFail(w, r, dErr)
		return
	case errors.As(qErr, &rl):
		a.playerFail(w, r, qErr)
		return
	}
	if parts[partState] {
		v[partState] = state
	}
	if dErr != nil {
		a.Log.WarnContext(r.Context(), "spotify devices failed", logging.Err(dErr))
	} else if parts[partDevices] {
		v[partDevices] = devices
	}
	if qErr != nil {
		a.Log.WarnContext(r.Context(), "spotify queue failed", logging.Err(qErr))
	} else if queue != nil {
		v[partQueue] = queue.Queue
	}
	writeJSON(w, http.StatusOK, v)
}

// searchPlayer serves GET /player/search?q=&limit= (tracks; q and limit
// are validated by SearchTracks).
func (a *API) searchPlayer(w http.ResponseWriter, r *http.Request) {
	limit := 8
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeValidation(w, map[string]string{"limit": "must be an integer"})
			return
		}
		limit = n
	}
	if !a.requireConnected(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), playerCallTimeout)
	defer cancel()
	items, err := a.playerClient().SearchTracks(ctx, r.URL.Query().Get("q"), limit)
	if err != nil {
		a.playerFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

// ─── commands ─────────────────────────────────────────────────────────────────

// playerCommand runs one command; detail is what the audit entry records.
// A *fieldError (a missing field or a bad body) answers 422; value checks
// are the Player's (*spotify.ArgError, also 422).
type playerCommand func(ctx context.Context, p playerControl, r *http.Request) (detail map[string]any, err error)

// fieldError is a request validation error on one body field.
type fieldError struct{ field, msg string }

func (e *fieldError) Error() string { return e.field + ": " + e.msg }

const msgNotConnected = "Spotify is not connected; authorize it in Settings → Spotify."

// playerAction wraps a command with the scope gate, a timeout, error
// mapping and an audit entry ("player.<name>", with "error": <code> when
// Spotify refused it; invalid requests are not audited).
func (a *API) playerAction(name string, cmd playerCommand) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connected, canControl, err := a.controlAccess(r.Context())
		if err != nil {
			a.internal(w, r, err)
			return
		}
		if !connected {
			writeError(w, http.StatusConflict, "spotify_not_connected", msgNotConnected)
			return
		}
		if !canControl {
			writeMissingScope(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), playerCallTimeout)
		defer cancel()
		detail, err := cmd(ctx, a.playerClient(), r)
		var fe *fieldError
		var ae *spotify.ArgError
		switch {
		case errors.As(err, &fe):
			writeValidation(w, map[string]string{fe.field: fe.msg})
			return
		case errors.As(err, &ae):
			writeValidation(w, map[string]string{ae.Field: ae.Msg})
			return
		}
		if detail == nil {
			detail = map[string]any{}
		}
		if err != nil {
			detail["error"] = a.playerFail(w, r, err)
			a.audit(r, "player."+name, detail)
			return
		}
		a.audit(r, "player."+name, detail)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func writeMissingScope(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "missing_scope",
		"The Spotify token cannot control playback. Re-authorize Spotify in Settings → Spotify to grant "+
			spotify.PlaybackControlScope+".")
}

// requireConnected answers 409 when Spotify is not connected.
func (a *API) requireConnected(w http.ResponseWriter, r *http.Request) bool {
	connected, _, err := a.controlAccess(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return false
	}
	if !connected {
		writeError(w, http.StatusConflict, "spotify_not_connected", msgNotConnected)
	}
	return connected
}

// playerFail maps a Spotify player error to an admin API error and returns
// the error code it answered.
func (a *API) playerFail(w http.ResponseWriter, r *http.Request, err error) string {
	var rl *spotify.RateLimitError
	var se *spotify.StatusError
	var ae *spotify.ArgError
	fail := func(status int, code, msg string) string {
		writeError(w, status, code, msg)
		return code
	}
	switch {
	case errors.As(err, &ae):
		writeValidation(w, map[string]string{ae.Field: ae.Msg})
		return "validation_failed"
	case errors.Is(err, spotify.ErrNoActiveDevice):
		return fail(http.StatusConflict, "no_active_device", "Open Spotify on a device first.")
	case errors.Is(err, spotify.ErrInsufficientScope):
		writeMissingScope(w)
		return "missing_scope"
	case errors.Is(err, spotify.ErrPremiumRequired):
		return fail(http.StatusForbidden, "premium_required", "Controlling playback needs Spotify Premium.")
	case errors.Is(err, spotify.ErrRestricted):
		return fail(http.StatusForbidden, "player_restricted",
			"Spotify refused the command (the device or the current context does not allow it).")
	case errors.Is(err, spotify.ErrNotFound):
		return fail(http.StatusNotFound, "not_found", "Spotify could not find that device or item.")
	case errors.Is(err, spotify.ErrNotConnected), errors.Is(err, spotify.ErrInvalidGrant):
		return fail(http.StatusConflict, "spotify_not_connected", msgNotConnected)
	case errors.As(err, &se) && se.Code == http.StatusUnauthorized:
		return fail(http.StatusConflict, "spotify_not_connected",
			"Spotify rejected the token even after a refresh; re-authorize Spotify in Settings → Spotify.")
	case errors.As(err, &rl):
		secs := max(1, int(math.Ceil(rl.RetryAfter.Seconds())))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		return fail(http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("Spotify is rate limiting requests; try again in %d s.", secs))
	case errors.Is(err, context.DeadlineExceeded):
		return fail(http.StatusGatewayTimeout, "spotify_timeout", "Spotify did not answer in time.")
	case errors.As(err, &se):
		a.Log.WarnContext(r.Context(), "spotify player call failed", slog.String("path", r.URL.Path), logging.Err(err))
		return fail(http.StatusBadGateway, "spotify_error", "Spotify answered with an error ("+strconv.Itoa(se.Code)+").")
	default:
		a.Log.WarnContext(r.Context(), "spotify player call failed", slog.String("path", r.URL.Path), logging.Err(err))
		return fail(http.StatusBadGateway, "spotify_error", "Could not reach Spotify.")
	}
}

// decodeOptional decodes a JSON body that may be empty.
func decodeOptional(r *http.Request, v any) error {
	if err := decode(r, v); err != nil && !errors.Is(err, io.EOF) {
		return &fieldError{"body", "invalid JSON body: " + err.Error()}
	}
	return nil
}

type deviceBody struct {
	DeviceID string `json:"device_id"`
}

// deviceDetail is the audit detail of a command with an optional device.
func deviceDetail(id string, kv ...any) map[string]any {
	d := map[string]any{}
	if id != "" {
		d["device_id"] = id
	}
	for i := 0; i+1 < len(kv); i += 2 {
		d[kv[i].(string)] = kv[i+1]
	}
	return d
}

// deviceOnly adapts a command that takes only an optional device id.
func deviceOnly(f func(playerControl, context.Context, string) error) playerCommand {
	return func(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
		var in deviceBody
		if err := decodeOptional(r, &in); err != nil {
			return nil, err
		}
		return deviceDetail(in.DeviceID), f(p, ctx, in.DeviceID)
	}
}
