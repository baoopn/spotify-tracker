package admin

import (
	"context"
	"net/http"

	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// The POST /player/* command bodies. device_id is optional everywhere but
// /player/transfer ("" = the active device). These only decode the body
// and check that required fields are present; the Player validates the
// values (a *spotify.ArgError names the field).

func (a *API) playerPlay(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
	var in struct {
		deviceBody
		URIs           []string `json:"uris"`
		ContextURI     string   `json:"context_uri"`
		OffsetPosition *int     `json:"offset_position"`
		OffsetURI      string   `json:"offset_uri"`
		PositionMS     *int64   `json:"position_ms"`
	}
	if err := decodeOptional(r, &in); err != nil {
		return nil, err
	}
	d := deviceDetail(in.DeviceID)
	if len(in.URIs) > 0 {
		d["uris"] = len(in.URIs)
		d["first_uri"] = in.URIs[0]
	}
	if in.ContextURI != "" {
		d["context_uri"] = in.ContextURI
	}
	return d, p.Play(ctx, spotify.PlayOptions{
		DeviceID: in.DeviceID, URIs: in.URIs, ContextURI: in.ContextURI,
		OffsetPosition: in.OffsetPosition, OffsetURI: in.OffsetURI, PositionMS: in.PositionMS,
	})
}

func (a *API) playerSeek(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
	var in struct {
		deviceBody
		PositionMS *int64 `json:"position_ms"`
	}
	if err := decodeOptional(r, &in); err != nil {
		return nil, err
	}
	if in.PositionMS == nil {
		return nil, &fieldError{"position_ms", "is required"}
	}
	return deviceDetail(in.DeviceID, "position_ms", *in.PositionMS), p.Seek(ctx, *in.PositionMS, in.DeviceID)
}

func (a *API) playerVolume(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
	var in struct {
		deviceBody
		VolumePercent *int `json:"volume_percent"`
	}
	if err := decodeOptional(r, &in); err != nil {
		return nil, err
	}
	if in.VolumePercent == nil {
		return nil, &fieldError{"volume_percent", "is required"}
	}
	return deviceDetail(in.DeviceID, "volume_percent", *in.VolumePercent),
		p.SetVolume(ctx, *in.VolumePercent, in.DeviceID)
}

func (a *API) playerShuffle(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
	var in struct {
		deviceBody
		State *bool `json:"state"`
	}
	if err := decodeOptional(r, &in); err != nil {
		return nil, err
	}
	if in.State == nil {
		return nil, &fieldError{"state", "is required (true or false)"}
	}
	return deviceDetail(in.DeviceID, "state", *in.State), p.SetShuffle(ctx, *in.State, in.DeviceID)
}

func (a *API) playerRepeat(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
	var in struct {
		deviceBody
		State string `json:"state"`
	}
	if err := decodeOptional(r, &in); err != nil {
		return nil, err
	}
	return deviceDetail(in.DeviceID, "state", in.State), p.SetRepeat(ctx, in.State, in.DeviceID)
}

func (a *API) playerTransfer(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
	var in struct {
		DeviceID string `json:"device_id"`
		// Play true starts playback on the device; false (the default)
		// keeps the current playing state.
		Play bool `json:"play"`
	}
	if err := decodeOptional(r, &in); err != nil {
		return nil, err
	}
	return map[string]any{"device_id": in.DeviceID, "play": in.Play}, p.Transfer(ctx, in.DeviceID, in.Play)
}

func (a *API) playerQueueAdd(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
	var in struct {
		deviceBody
		URI string `json:"uri"`
	}
	if err := decodeOptional(r, &in); err != nil {
		return nil, err
	}
	return deviceDetail(in.DeviceID, "uri", in.URI), p.AddToQueue(ctx, in.URI, in.DeviceID)
}
