package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/observability"
)

// TokenSource supplies access tokens. force=true asks for a fresh token
// (after a 401). The poller's implementation calls the API over gRPC.
type TokenSource interface {
	AccessToken(ctx context.Context, force bool) (string, error)
}

// Player reads the user's playback state.
type Player struct {
	tokens  TokenSource
	apiBase string
	http    *http.Client
}

// NewPlayer creates a Player. apiBase may be empty for the real API.
func NewPlayer(tokens TokenSource, apiBase string, hc *http.Client) *Player {
	if apiBase == "" {
		apiBase = defaultAPIBase
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Player{tokens: tokens, apiBase: strings.TrimRight(apiBase, "/"), http: hc}
}

type currentlyPlayingResponse struct {
	Timestamp  int64  `json:"timestamp"`
	IsPlaying  bool   `json:"is_playing"`
	ProgressMS *int32 `json:"progress_ms"`
	Type       string `json:"currently_playing_type"`
	Context    *struct {
		Type         string       `json:"type"`
		URI          string       `json:"uri"`
		ExternalURLs externalURLs `json:"external_urls"`
	} `json:"context"`
	Item *trackObject `json:"item"`
}

// trackObject is a Spotify track object (currently-playing item, queue entry).
type trackObject struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URI         string `json:"uri"`
	DurationMS  int32  `json:"duration_ms"`
	Explicit    bool   `json:"explicit"`
	TrackNumber int32  `json:"track_number"`
	DiscNumber  int32  `json:"disc_number"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
	ExternalURLs externalURLs `json:"external_urls"`
	Album        struct {
		ID           string       `json:"id"`
		Name         string       `json:"name"`
		ReleaseDate  string       `json:"release_date"`
		URI          string       `json:"uri"`
		ExternalURLs externalURLs `json:"external_urls"`
		AlbumType    string       `json:"album_type"`
		TotalTracks  int32        `json:"total_tracks"`
		Precision    string       `json:"release_date_precision"`
		Images       []struct {
			URL string `json:"url"`
		} `json:"images"`
	} `json:"album"`
	Artists []struct {
		ID           string       `json:"id"`
		Name         string       `json:"name"`
		URI          string       `json:"uri"`
		ExternalURLs externalURLs `json:"external_urls"`
	} `json:"artists"`
}

// entry converts the track object to the domain catalog entry.
func (it *trackObject) entry() domain.CatalogEntry {
	e := domain.CatalogEntry{
		Track: domain.Track{
			ID: it.ID, Name: it.Name, URI: it.URI, ISRC: it.ExternalIDs.ISRC,
			DurationMS: it.DurationMS, Explicit: it.Explicit, ExternalURL: it.ExternalURLs.Spotify,
			AlbumID:     it.Album.ID,
			TrackNumber: domain.PositiveInt32(it.TrackNumber), DiscNumber: domain.PositiveInt32(it.DiscNumber),
		},
		Album: domain.Album{
			ID: it.Album.ID, Name: it.Album.Name, ReleaseDate: it.Album.ReleaseDate,
			URI: it.Album.URI, ExternalURL: it.Album.ExternalURLs.Spotify,
			AlbumType:            domain.NormalizeAlbumType(it.Album.AlbumType),
			TotalTracks:          domain.PositiveInt32(it.Album.TotalTracks),
			ReleaseDatePrecision: domain.NormalizeReleaseDatePrecision(it.Album.Precision),
		},
	}
	if len(it.Album.Images) > 0 {
		e.Album.CoverImageURL = it.Album.Images[0].URL
	}
	for _, a := range it.Artists {
		if a.ID == "" {
			continue
		}
		e.Artists = append(e.Artists, domain.Artist{
			ID: a.ID, Name: a.Name, URI: a.URI, ExternalURL: a.ExternalURLs.Spotify,
		})
	}
	return e
}

type externalURLs struct {
	Spotify string `json:"spotify"`
}

// CurrentlyPlaying returns the user's current track, or nil when nothing is
// playing (HTTP 204, an empty item, or non-track content such as podcasts).
// FetchedAt is the midpoint of the HTTP request that answered.
func (p *Player) CurrentlyPlaying(ctx context.Context) (*domain.Playback, error) {
	pb, _, err := p.CurrentlyPlayingTimed(ctx)
	return pb, err
}

// RequestTime is when the HTTP request that answered was sent and when its
// response headers arrived. A token lookup or a retry after a 401 is not
// part of it.
type RequestTime struct{ Sent, Received time.Time }

// Mid is the midpoint of the request, in UTC (zero when not measured).
func (t RequestTime) Mid() time.Time {
	if t.Sent.IsZero() {
		return time.Time{}
	}
	return t.Sent.Add(t.Received.Sub(t.Sent) / 2).UTC()
}

// CurrentlyPlayingTimed is CurrentlyPlaying plus the timing of the request
// that answered (also when nothing is playing).
func (p *Player) CurrentlyPlayingTimed(ctx context.Context) (*domain.Playback, RequestTime, error) {
	force := false
	for attempt := 0; attempt < 2; attempt++ {
		pb, rt, unauthorized, err := p.currentlyPlaying(ctx, force)
		if unauthorized {
			force = true
			continue
		}
		return pb, rt, err
	}
	return nil, RequestTime{}, errors.New("spotify: unauthorized after token refresh")
}

func (p *Player) currentlyPlaying(ctx context.Context, force bool) (*domain.Playback, RequestTime, bool, error) {
	var rt RequestTime
	tok, err := p.tokens.AccessToken(ctx, force)
	if err != nil {
		return nil, rt, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiBase+"/me/player/currently-playing", nil)
	if err != nil {
		return nil, rt, false, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	// Time only the request itself: Spotify reads progress_ms during it.
	rt.Sent = time.Now()
	resp, err := p.http.Do(req)
	rt.Received = time.Now()
	if err != nil {
		return nil, rt, false, fmt.Errorf("spotify: currently-playing: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	observability.UpstreamRequests.WithLabelValues("spotify", strconv.Itoa(resp.StatusCode)).Inc()
	fetchedAt := rt.Mid()

	switch {
	case resp.StatusCode == http.StatusNoContent:
		return nil, rt, false, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, rt, true, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, rt, false, rateLimitFrom(resp)
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, rt, false, fmt.Errorf("spotify: currently-playing failed (%d): %s", resp.StatusCode, truncate(body))
	}

	var cp currentlyPlayingResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&cp); err != nil {
		return nil, rt, false, fmt.Errorf("spotify: decode currently-playing: %w", err)
	}
	if cp.Item == nil || cp.Item.ID == "" || (cp.Type != "" && cp.Type != "track") {
		return nil, rt, false, nil
	}
	e := cp.Item.entry()
	pb := &domain.Playback{
		Track: e.Track, Album: e.Album, Artists: e.Artists,
		IsPlaying:  cp.IsPlaying,
		ProgressMS: cp.ProgressMS,
		FetchedAt:  fetchedAt,
		PlayedAt:   fetchedAt,
	}
	if c := cp.Context; c != nil && c.URI != "" {
		pb.Context = &domain.PlaybackContext{Type: c.Type, URI: c.URI, ExternalURL: c.ExternalURLs.Spotify}
	}
	if cp.Timestamp > 0 {
		// The last playback state change, not the response time.
		at := time.UnixMilli(cp.Timestamp).UTC()
		pb.PlayedAt, pb.ChangedAt = at, &at
	}
	return pb, rt, false, nil
}

func rateLimitFrom(resp *http.Response) error {
	retry := 30 * time.Second
	if v := resp.Header.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			retry = time.Duration(n) * time.Second
		}
	}
	var body struct {
		Error struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	return &RateLimitError{RetryAfter: retry, Reason: body.Error.Reason}
}
