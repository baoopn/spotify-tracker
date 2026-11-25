package spotify

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Player commands need user-modify-playback-state (PlaybackControlScope)
// and a Premium account. deviceID "" targets the active device.

// PlayOptions are the optional parts of a Play call. With neither URIs nor
// ContextURI set, Play resumes the current playback.
type PlayOptions struct {
	DeviceID string
	// ContextURI is an album, playlist, artist or show URI.
	ContextURI string
	// URIs are track URIs to play (at most 100), instead of a context.
	URIs []string
	// OffsetPosition or OffsetURI picks where in the context or URIs to
	// start (one of them at most).
	OffsetPosition *int
	OffsetURI      string
	// PositionMS seeks within the first track.
	PositionMS *int64
}

// MaxPlayURIs is the most track URIs one Play call takes.
const MaxPlayURIs = 100

// body validates o and builds the request body. All play validation
// lives here; the admin API only decodes the request.
func (o PlayOptions) body() (map[string]any, error) {
	b := map[string]any{}
	if o.ContextURI != "" && len(o.URIs) > 0 {
		return nil, argErr("uris", "pass a context URI or track URIs, not both")
	}
	if o.ContextURI != "" {
		if !validContextURI(o.ContextURI) {
			return nil, argErr("context_uri", "must be an album, playlist, artist or show URI")
		}
		b["context_uri"] = o.ContextURI
	}
	if len(o.URIs) > MaxPlayURIs {
		return nil, argErr("uris", "at most %d tracks", MaxPlayURIs)
	}
	if len(o.URIs) > 0 {
		uris := make([]string, len(o.URIs))
		for i, u := range o.URIs {
			v, ok := TrackURI(u)
			if !ok {
				return nil, argErr("uris", "must be Spotify track ids or URIs")
			}
			uris[i] = v
		}
		b["uris"] = uris
	}
	switch {
	case o.OffsetPosition != nil && o.OffsetURI != "":
		return nil, argErr("offset_uri", "pass an offset position or an offset URI, not both")
	case o.OffsetPosition != nil:
		if o.ContextURI == "" && len(o.URIs) == 0 {
			return nil, argErr("offset_position", "needs a context URI or track URIs")
		}
		if *o.OffsetPosition < 0 {
			return nil, argErr("offset_position", "must be 0 or more")
		}
		b["offset"] = map[string]any{"position": *o.OffsetPosition}
	case o.OffsetURI != "":
		if o.ContextURI == "" && len(o.URIs) == 0 {
			return nil, argErr("offset_uri", "needs a context URI or track URIs")
		}
		v, ok := TrackURI(o.OffsetURI)
		if !ok {
			return nil, argErr("offset_uri", "must be a Spotify track id or URI")
		}
		b["offset"] = map[string]any{"uri": v}
	}
	if o.PositionMS != nil {
		if *o.PositionMS < 0 {
			return nil, argErr("position_ms", "must be 0 or more")
		}
		b["position_ms"] = *o.PositionMS
	}
	return b, nil
}

// Play starts or resumes playback.
func (p *Player) Play(ctx context.Context, o PlayOptions) error {
	body, err := o.body()
	if err != nil {
		return err
	}
	q, err := deviceQuery(o.DeviceID, nil)
	if err != nil {
		return err
	}
	var payload any
	if len(body) > 0 {
		payload = body
	}
	_, err = p.call(ctx, "play", http.MethodPut, "/me/player/play", q, payload, nil)
	return err
}

// Pause pauses playback.
func (p *Player) Pause(ctx context.Context, deviceID string) error {
	return p.command(ctx, "pause", http.MethodPut, "/me/player/pause", deviceID, nil)
}

// Next skips to the next track.
func (p *Player) Next(ctx context.Context, deviceID string) error {
	return p.command(ctx, "next", http.MethodPost, "/me/player/next", deviceID, nil)
}

// Previous skips to the previous track.
func (p *Player) Previous(ctx context.Context, deviceID string) error {
	return p.command(ctx, "previous", http.MethodPost, "/me/player/previous", deviceID, nil)
}

// Seek moves to positionMS in the current track (past the end skips to
// the next track).
func (p *Player) Seek(ctx context.Context, positionMS int64, deviceID string) error {
	if positionMS < 0 {
		return argErr("position_ms", "must be 0 or more")
	}
	return p.command(ctx, "seek", http.MethodPut, "/me/player/seek", deviceID,
		url.Values{"position_ms": {strconv.FormatInt(positionMS, 10)}})
}

// SetVolume sets the device volume (0–100).
func (p *Player) SetVolume(ctx context.Context, percent int, deviceID string) error {
	if percent < 0 || percent > 100 {
		return argErr("volume_percent", "must be between 0 and 100")
	}
	return p.command(ctx, "volume", http.MethodPut, "/me/player/volume", deviceID,
		url.Values{"volume_percent": {strconv.Itoa(percent)}})
}

// SetShuffle turns shuffle on or off.
func (p *Player) SetShuffle(ctx context.Context, on bool, deviceID string) error {
	return p.command(ctx, "shuffle", http.MethodPut, "/me/player/shuffle", deviceID,
		url.Values{"state": {strconv.FormatBool(on)}})
}

// SetRepeat sets the repeat mode: RepeatOff, RepeatTrack or RepeatContext.
func (p *Player) SetRepeat(ctx context.Context, mode, deviceID string) error {
	if !validRepeat(mode) {
		return argErr("state", "must be off, track or context")
	}
	return p.command(ctx, "repeat", http.MethodPut, "/me/player/repeat", deviceID,
		url.Values{"state": {mode}})
}

// Transfer moves playback to deviceID; play true starts it there, false
// keeps the current playing state.
func (p *Player) Transfer(ctx context.Context, deviceID string, play bool) error {
	if err := ValidateDeviceID(deviceID); err != nil {
		return err
	}
	_, err := p.call(ctx, "transfer", http.MethodPut, "/me/player", nil,
		map[string]any{"device_ids": []string{deviceID}, "play": play}, nil)
	return err
}

// AddToQueue adds a track (id or URI) to the end of the queue.
func (p *Player) AddToQueue(ctx context.Context, track, deviceID string) error {
	uri, ok := TrackURI(track)
	if !ok {
		return argErr("uri", "must be a Spotify track id or URI")
	}
	return p.command(ctx, "add to queue", http.MethodPost, "/me/player/queue", deviceID,
		url.Values{"uri": {uri}})
}

// command sends a bodiless player command with query values extra.
func (p *Player) command(ctx context.Context, what, method, path, deviceID string, extra url.Values) error {
	q, err := deviceQuery(deviceID, extra)
	if err != nil {
		return err
	}
	_, err = p.call(ctx, what, method, path, q, nil, nil)
	return err
}
