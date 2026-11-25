package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

type playerView struct {
	Connected  bool                   `json:"connected"`
	CanControl bool                   `json:"can_control"`
	State      *spotify.PlaybackState `json:"state"`
	Devices    []spotify.Device       `json:"devices"`
	Queue      []spotify.PlayerTrack  `json:"queue"`
}

// getPlayer serves GET /player: the playback state, devices and queue.
// The devices and queue are best effort (null when Spotify refuses them).
func (a *API) getPlayer(w http.ResponseWriter, r *http.Request) {
	connected, canControl, err := a.controlAccess(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	v := playerView{Connected: connected, CanControl: canControl}
	if !connected {
		writeJSON(w, http.StatusOK, v)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), playerCallTimeout)
	defer cancel()
	p := a.playerClient()

	var (
		wg                sync.WaitGroup
		stErr, dErr, qErr error
		queue             *spotify.PlayerQueue
	)
	wg.Add(3)
	go func() { defer wg.Done(); v.State, stErr = p.PlaybackState(ctx) }()
	go func() { defer wg.Done(); v.Devices, dErr = p.Devices(ctx) }()
	go func() { defer wg.Done(); queue, qErr = p.PlayerQueue(ctx, playerQueueShown) }()
	wg.Wait()

	if stErr != nil {
		a.playerFail(w, r, stErr)
		return
	}
	if dErr != nil {
		v.Devices = nil
		a.Log.WarnContext(r.Context(), "spotify devices failed", logging.Err(dErr))
	}
	if qErr != nil {
		a.Log.WarnContext(r.Context(), "spotify queue failed", logging.Err(qErr))
	}
	// Spotify answers the queue for the last device even when nothing is
	// active; only show it next to a loaded track.
	if qErr == nil && queue != nil && v.State != nil && v.State.Item != nil {
		v.Queue = queue.Queue
	}
	writeJSON(w, http.StatusOK, v)
}

// searchPlayer serves GET /player/search?q=&limit= (tracks, limit 1–10).
func (a *API) searchPlayer(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len([]rune(q)) > 200 {
		writeValidation(w, map[string]string{"q": "must have 1 to 200 characters"})
		return
	}
	limit, err := intParam(r, "limit", 8, 1, 10)
	if err != nil {
		writeValidation(w, map[string]string{"limit": err.Error()})
		return
	}
	if !a.requireConnected(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), playerCallTimeout)
	defer cancel()
	items, err := a.playerClient().SearchTracks(ctx, q, limit)
	if err != nil {
		a.playerFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

// ─── commands ─────────────────────────────────────────────────────────────────

// playerCommand runs one command; detail is what the audit entry records.
// A *fieldError answers 422.
type playerCommand func(ctx context.Context, p playerControl, r *http.Request) (detail map[string]any, err error)

// fieldError is a request validation error on one body field.
type fieldError struct{ field, msg string }

func (e *fieldError) Error() string { return e.field + ": " + e.msg }

// playerAction wraps a command with the scope gate, a timeout, error
// mapping and an audit entry ("player.<name>").
func (a *API) playerAction(name string, cmd playerCommand) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connected, canControl, err := a.controlAccess(r.Context())
		if err != nil {
			a.internal(w, r, err)
			return
		}
		if !connected {
			writeError(w, http.StatusConflict, "spotify_not_connected",
				"Spotify is not connected; authorize it in Settings → Spotify.")
			return
		}
		if !canControl {
			writeError(w, http.StatusConflict, "missing_scope",
				"The Spotify token cannot control playback. Re-authorize Spotify in Settings → Spotify to grant "+
					spotify.PlaybackControlScope+".")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), playerCallTimeout)
		defer cancel()
		detail, err := cmd(ctx, a.playerClient(), r)
		var fe *fieldError
		switch {
		case errors.As(err, &fe):
			writeValidation(w, map[string]string{fe.field: fe.msg})
			return
		case err != nil:
			a.playerFail(w, r, err)
			return
		}
		a.audit(r, "player."+name, detail)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// requireConnected answers 409 when Spotify is not connected.
func (a *API) requireConnected(w http.ResponseWriter, r *http.Request) bool {
	connected, _, err := a.controlAccess(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return false
	}
	if !connected {
		writeError(w, http.StatusConflict, "spotify_not_connected",
			"Spotify is not connected; authorize it in Settings → Spotify.")
	}
	return connected
}

// playerFail maps a Spotify player error to an admin API error.
func (a *API) playerFail(w http.ResponseWriter, r *http.Request, err error) {
	var rl *spotify.RateLimitError
	var se *spotify.StatusError
	switch {
	case errors.Is(err, spotify.ErrInvalidArgument):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed",
			strings.TrimPrefix(err.Error(), spotify.ErrInvalidArgument.Error()+": "))
	case errors.Is(err, spotify.ErrNoActiveDevice):
		writeError(w, http.StatusConflict, "no_active_device", "Open Spotify on a device first.")
	case errors.Is(err, spotify.ErrPremiumRequired):
		writeError(w, http.StatusForbidden, "premium_required", "Controlling playback needs Spotify Premium.")
	case errors.Is(err, spotify.ErrRestricted):
		writeError(w, http.StatusForbidden, "player_restricted",
			"Spotify refused the command (the device or the current context does not allow it).")
	case errors.Is(err, spotify.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Spotify could not find that device or item.")
	case errors.Is(err, spotify.ErrNotConnected), errors.Is(err, spotify.ErrInvalidGrant):
		writeError(w, http.StatusConflict, "spotify_not_connected",
			"Spotify is not connected; authorize it in Settings → Spotify.")
	case errors.As(err, &rl):
		w.Header().Set("Retry-After", strconv.Itoa(int(rl.RetryAfter.Seconds())))
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("Spotify is rate limiting requests; try again in %s.", rl.RetryAfter.Round(time.Second)))
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "spotify_timeout", "Spotify did not answer in time.")
	case errors.As(err, &se):
		a.Log.WarnContext(r.Context(), "spotify player call failed", slog.String("path", r.URL.Path), logging.Err(err))
		writeError(w, http.StatusBadGateway, "spotify_error", "Spotify answered with an error ("+strconv.Itoa(se.Code)+").")
	default:
		a.Log.WarnContext(r.Context(), "spotify player call failed", slog.String("path", r.URL.Path), logging.Err(err))
		writeError(w, http.StatusBadGateway, "spotify_error", "Could not reach Spotify.")
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

func (b deviceBody) validate() error {
	if b.DeviceID != "" && spotify.ValidateDeviceID(b.DeviceID) != nil {
		return &fieldError{"device_id", "is not a valid device id"}
	}
	return nil
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
		if err := in.validate(); err != nil {
			return nil, err
		}
		return deviceDetail(in.DeviceID), f(p, ctx, in.DeviceID)
	}
}
