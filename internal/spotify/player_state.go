package spotify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Device is a Spotify Connect device.
type Device struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	VolumePercent    *int   `json:"volume_percent"`
	SupportsVolume   bool   `json:"supports_volume"`
	IsActive         bool   `json:"is_active"`
	IsPrivateSession bool   `json:"is_private_session"`
	IsRestricted     bool   `json:"is_restricted"`
}

// PlayerArtist is an artist of a PlayerTrack.
type PlayerArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PlayerTrack is a track as the admin player shows it.
type PlayerTrack struct {
	ID            string         `json:"id"`
	URI           string         `json:"uri"`
	Name          string         `json:"name"`
	DurationMS    int32          `json:"duration_ms"`
	Explicit      bool           `json:"explicit"`
	Artists       []PlayerArtist `json:"artists"`
	AlbumName     string         `json:"album_name"`
	CoverImageURL string         `json:"cover_image_url"`
	ExternalURL   string         `json:"external_url"`
}

// PlayerContext is what the player plays from (album, playlist, …).
type PlayerContext struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
}

// PlaybackState is the full state of the user's player (GET /me/player).
type PlaybackState struct {
	IsPlaying    bool           `json:"is_playing"`
	ProgressMS   int64          `json:"progress_ms"`
	ShuffleState bool           `json:"shuffle_state"`
	RepeatState  string         `json:"repeat_state"`
	Device       *Device        `json:"device"`
	Context      *PlayerContext `json:"context"`
	// Type is "track", "episode", "ad" or "unknown"; Item is nil unless
	// a track is loaded.
	Type string       `json:"currently_playing_type"`
	Item *PlayerTrack `json:"item"`
	// Disallows lists the actions the current context does not allow
	// (e.g. "pausing", "skipping_prev", "toggling_shuffle").
	Disallows map[string]bool `json:"disallows"`
}

// PlayerQueue is the current track and the tracks after it.
type PlayerQueue struct {
	CurrentlyPlaying *PlayerTrack  `json:"currently_playing"`
	Queue            []PlayerTrack `json:"queue"`
}

// playerTrack converts a track object. thumb picks a smaller cover (the
// second image, ~300 px) for lists.
func (it *trackObject) playerTrack(thumb bool) PlayerTrack {
	t := PlayerTrack{
		ID: it.ID, URI: it.URI, Name: it.Name, DurationMS: it.DurationMS, Explicit: it.Explicit,
		AlbumName: it.Album.Name, ExternalURL: it.ExternalURLs.Spotify, Artists: []PlayerArtist{},
	}
	if t.URI == "" && trackIDPattern.MatchString(it.ID) {
		t.URI = "spotify:track:" + it.ID
	}
	if imgs := it.Album.Images; len(imgs) > 0 {
		t.CoverImageURL = imgs[0].URL
		if thumb && len(imgs) > 1 {
			t.CoverImageURL = imgs[1].URL
		}
	}
	for _, a := range it.Artists {
		t.Artists = append(t.Artists, PlayerArtist{ID: a.ID, Name: a.Name})
	}
	return t
}

type playbackStateResponse struct {
	IsPlaying    bool           `json:"is_playing"`
	ProgressMS   *int64         `json:"progress_ms"`
	ShuffleState bool           `json:"shuffle_state"`
	RepeatState  string         `json:"repeat_state"`
	Device       *Device        `json:"device"`
	Context      *PlayerContext `json:"context"`
	Type         string         `json:"currently_playing_type"`
	Item         *trackObject   `json:"item"`
	Actions      struct {
		Disallows map[string]bool `json:"disallows"`
	} `json:"actions"`
}

// PlaybackState returns the player state, or nil when there is no active
// device (HTTP 204).
func (p *Player) PlaybackState(ctx context.Context) (*PlaybackState, error) {
	var r playbackStateResponse
	code, err := p.call(ctx, "playback state", http.MethodGet, "/me/player",
		url.Values{"additional_types": {"track"}}, nil, &r)
	if err != nil || code != http.StatusOK {
		return nil, err
	}
	st := &PlaybackState{
		IsPlaying: r.IsPlaying, ShuffleState: r.ShuffleState, RepeatState: r.RepeatState,
		Device: r.Device, Type: r.Type, Disallows: map[string]bool{},
	}
	if st.RepeatState == "" {
		st.RepeatState = RepeatOff
	}
	if r.ProgressMS != nil {
		st.ProgressMS = *r.ProgressMS
	}
	if r.Context != nil && r.Context.URI != "" {
		st.Context = r.Context
	}
	if r.Item != nil && r.Item.ID != "" && (r.Type == "" || r.Type == "track") {
		t := r.Item.playerTrack(false)
		st.Item = &t
	}
	for k, v := range r.Actions.Disallows {
		if v {
			st.Disallows[k] = true
		}
	}
	return st, nil
}

// Devices lists the user's available Spotify Connect devices.
func (p *Player) Devices(ctx context.Context) ([]Device, error) {
	var r struct {
		Devices []Device `json:"devices"`
	}
	if _, err := p.call(ctx, "devices", http.MethodGet, "/me/player/devices", nil, nil, &r); err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(r.Devices))
	for _, d := range r.Devices {
		if d.ID != "" { // devices without an id cannot be targeted
			out = append(out, d)
		}
	}
	return out, nil
}

// PlayerQueue returns the current track and the next limit tracks of the
// queue, in order. Unlike Queue (used by the poller's prefetch) it keeps
// repeats; episodes and entries without a playable URI are skipped.
func (p *Player) PlayerQueue(ctx context.Context, limit int) (*PlayerQueue, error) {
	var r struct {
		CurrentlyPlaying *queueItem  `json:"currently_playing"`
		Queue            []queueItem `json:"queue"`
	}
	if _, err := p.call(ctx, "queue", http.MethodGet, "/me/player/queue", nil, nil, &r); err != nil {
		return nil, err
	}
	out := &PlayerQueue{Queue: []PlayerTrack{}}
	if c := r.CurrentlyPlaying; c != nil && playerQueueItem(c) {
		t := c.playerTrack(true)
		out.CurrentlyPlaying = &t
	}
	for i := range r.Queue {
		if len(out.Queue) >= limit {
			break
		}
		if it := &r.Queue[i]; playerQueueItem(it) {
			out.Queue = append(out.Queue, it.playerTrack(true))
		}
	}
	return out, nil
}

func playerQueueItem(it *queueItem) bool {
	return (it.Type == "" || it.Type == "track") && it.Name != "" &&
		(it.URI != "" || trackIDPattern.MatchString(it.ID))
}

// SearchTracks searches the catalog for tracks (limit 1–10).
func (p *Player) SearchTracks(ctx context.Context, q string, limit int) ([]PlayerTrack, error) {
	q = strings.TrimSpace(q)
	if q == "" || utf8.RuneCountInString(q) > 200 {
		return nil, fmt.Errorf("%w: the search must have 1 to 200 characters", ErrInvalidArgument)
	}
	if limit < 1 || limit > 10 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 10", ErrInvalidArgument)
	}
	var r struct {
		Tracks struct {
			Items []trackObject `json:"items"`
		} `json:"tracks"`
	}
	qv := url.Values{"q": {q}, "type": {"track"}, "limit": {strconv.Itoa(limit)}}
	if _, err := p.call(ctx, "search", http.MethodGet, "/search", qv, nil, &r); err != nil {
		return nil, err
	}
	out := make([]PlayerTrack, 0, len(r.Tracks.Items))
	for i := range r.Tracks.Items {
		if it := &r.Tracks.Items[i]; trackIDPattern.MatchString(it.ID) && it.Name != "" {
			out = append(out, it.playerTrack(true))
		}
	}
	return out, nil
}
