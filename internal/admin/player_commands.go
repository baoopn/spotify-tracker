package admin

import (
	"context"
	"net/http"

	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// The POST /player/* command bodies. device_id is optional everywhere but
// /player/transfer ("" = the active device).

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
	if err := in.validate(); err != nil {
		return nil, err
	}
	if len(in.URIs) > spotify.MaxPlayURIs {
		return nil, &fieldError{"uris", "at most 100 tracks"}
	}
	for _, u := range in.URIs {
		if _, err := spotify.TrackURI(u); err != nil {
			return nil, &fieldError{"uris", "must be Spotify track ids or URIs"}
		}
	}
	if in.ContextURI != "" && spotify.ValidateContextURI(in.ContextURI) != nil {
		return nil, &fieldError{"context_uri", "must be an album, playlist, artist or show URI"}
	}
	o := spotify.PlayOptions{
		DeviceID: in.DeviceID, URIs: in.URIs, ContextURI: in.ContextURI,
		OffsetPosition: in.OffsetPosition, OffsetURI: in.OffsetURI, PositionMS: in.PositionMS,
	}
	d := deviceDetail(in.DeviceID)
	if len(in.URIs) > 0 {
		d["uris"] = in.URIs
	}
	if in.ContextURI != "" {
		d["context_uri"] = in.ContextURI
	}
	return d, p.Play(ctx, o)
}

func (a *API) playerSeek(ctx context.Context, p playerControl, r *http.Request) (map[string]any, error) {
	var in struct {
		deviceBody
		PositionMS *int64 `json:"position_ms"`
	}
	if err := decodeOptional(r, &in); err != nil {
		return nil, err
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	if in.PositionMS == nil || *in.PositionMS < 0 {
		return nil, &fieldError{"position_ms", "is required and must be 0 or more"}
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
	if err := in.validate(); err != nil {
		return nil, err
	}
	if in.VolumePercent == nil || *in.VolumePercent < 0 || *in.VolumePercent > 100 {
		return nil, &fieldError{"volume_percent", "is required and must be between 0 and 100"}
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
	if err := in.validate(); err != nil {
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
	if err := in.validate(); err != nil {
		return nil, err
	}
	if !spotify.ValidRepeat(in.State) {
		return nil, &fieldError{"state", "must be off, track or context"}
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
	if spotify.ValidateDeviceID(in.DeviceID) != nil {
		return nil, &fieldError{"device_id", "is required and must be a valid device id"}
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
	if err := in.validate(); err != nil {
		return nil, err
	}
	uri, err := spotify.TrackURI(in.URI)
	if err != nil {
		return nil, &fieldError{"uri", "must be a Spotify track id or URI"}
	}
	return deviceDetail(in.DeviceID, "uri", uri), p.AddToQueue(ctx, uri, in.DeviceID)
}
