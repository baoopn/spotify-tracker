// Package soundcharts is a minimal client for the Soundcharts song-by-ISRC
// endpoint, which provides audio features and genres.
package soundcharts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/observability"
)

// Song is the subset of the Soundcharts song object we use.
type Song struct {
	Features domain.AudioFeatures
	Genres   []domain.Genre
	// HasAudio is false when Soundcharts knows the song but has no audio data.
	HasAudio bool
}

// RetryableError marks transient failures (429/5xx) the caller should retry.
type RetryableError struct {
	Status     int
	RetryAfter time.Duration
	Body       string
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("soundcharts: transient error (%d): %s", e.Status, e.Body)
}

// Client calls the Soundcharts API with a single credential pair and a
// client-side rate limit shared by all goroutines.
type Client struct {
	base    string
	appID   string
	token   string
	http    *http.Client
	limiter *rate.Limiter
	// pausedUntil (unix nanos) blocks all requests after a 429 with Retry-After.
	pausedUntil atomic.Int64
}

// New creates a Client. rps/burst bound the request rate across goroutines.
func New(baseURL, appID, token string, rps float64, burst int, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	if burst < 1 {
		burst = 1
	}
	return &Client{
		base: strings.TrimRight(baseURL, "/"), appID: appID, token: token, http: hc,
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

func (c *Client) wait(ctx context.Context) error {
	if until := time.Unix(0, c.pausedUntil.Load()); time.Now().Before(until) {
		t := time.NewTimer(time.Until(until))
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return c.limiter.Wait(ctx)
}

type songResponse struct {
	Object *struct {
		Audio *struct {
			Acousticness     *float64 `json:"acousticness"`
			Danceability     *float64 `json:"danceability"`
			Energy           *float64 `json:"energy"`
			Instrumentalness *float64 `json:"instrumentalness"`
			Key              *int32   `json:"key"`
			Liveness         *float64 `json:"liveness"`
			Loudness         *float64 `json:"loudness"`
			Mode             *int32   `json:"mode"`
			Speechiness      *float64 `json:"speechiness"`
			Tempo            *float64 `json:"tempo"`
			TimeSignature    *int32   `json:"timeSignature"`
			Valence          *float64 `json:"valence"`
		} `json:"audio"`
		Genres []struct {
			Root string   `json:"root"`
			Sub  []string `json:"sub"`
		} `json:"genres"`
	} `json:"object"`
}

// SongByISRC fetches a song. It returns (nil, nil) when the ISRC is unknown.
func (c *Client) SongByISRC(ctx context.Context, isrc string) (*Song, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	u := c.base + "/api/v2.25/song/by-isrc/" + url.PathEscape(isrc)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-app-id", c.appID)
	req.Header.Set("x-api-key", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &RetryableError{Status: 0, Body: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	observability.UpstreamRequests.WithLabelValues("soundcharts", strconv.Itoa(resp.StatusCode)).Inc()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, &RetryableError{Status: resp.StatusCode, Body: err.Error()}
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		re := &RetryableError{Status: resp.StatusCode, Body: snippet(body)}
		if v, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			re.RetryAfter = time.Duration(v) * time.Second
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			pause := re.RetryAfter
			if pause <= 0 {
				pause = 10 * time.Second
			}
			c.pausedUntil.Store(time.Now().Add(pause).UnixNano())
		}
		return nil, re
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("soundcharts: request failed (%d): %s", resp.StatusCode, snippet(body))
	}

	var sr songResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("soundcharts: decode: %w", err)
	}
	if sr.Object == nil {
		return nil, nil
	}
	song := &Song{Genres: []domain.Genre{}}
	if a := sr.Object.Audio; a != nil {
		song.HasAudio = true
		song.Features = domain.AudioFeatures{
			Acousticness: a.Acousticness, Danceability: a.Danceability, Energy: a.Energy,
			Instrumentalness: a.Instrumentalness, Key: a.Key, Liveness: a.Liveness,
			Loudness: a.Loudness, Mode: a.Mode, Speechiness: a.Speechiness, Tempo: a.Tempo,
			TimeSignature: a.TimeSignature, Valence: a.Valence,
		}
	}
	for _, g := range sr.Object.Genres {
		if g.Root == "" {
			continue
		}
		sub := g.Sub
		if sub == nil {
			sub = []string{}
		}
		song.Genres = append(song.Genres, domain.Genre{Root: g.Root, Sub: sub})
	}
	return song, nil
}

func snippet(b []byte) string {
	const max = 300
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
