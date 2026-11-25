// Package lrclib is a small client for the LRCLIB lyrics API
// (https://lrclib.net/docs). It needs no API key; requests are rate limited
// and carry a User-Agent naming the project, as LRCLIB asks.
package lrclib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// DefaultBaseURL is the public LRCLIB instance.
const DefaultBaseURL = "https://lrclib.net"

// UserAgent identifies the client to LRCLIB.
const UserAgent = "spotify-tracker (https://github.com/baoopn/spotify-tracker)"

const (
	maxBody       = 4 << 20 // response bodies are read up to 4 MiB
	maxText       = 200_000 // stored lyrics are capped at this many bytes
	durationSlack = 2.0     // seconds a search candidate may differ from the track
	defaultRetry  = time.Minute
	maxRetryAfter = time.Hour
)

// DefaultTimeout is the request timeout of a client made with a nil
// http.Client. config checks LYRICS_LEASE against it.
const DefaultTimeout = 20 * time.Second

// Client calls the LRCLIB API. It is safe for concurrent use.
type Client struct {
	base string
	http *http.Client
	lim  *rate.Limiter
}

// New returns a client for baseURL (empty = DefaultBaseURL) that sends at
// most one request per minInterval (<= 0 = no limit). A nil hc uses a client
// with a 20 s timeout.
func New(baseURL string, minInterval time.Duration, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	limit := rate.Inf
	if minInterval > 0 {
		limit = rate.Every(minInterval)
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), http: hc, lim: rate.NewLimiter(limit, 1)}
}

// Record is one LRCLIB lyrics record.
type Record struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  *string `json:"plainLyrics"`
	SyncedLyrics *string `json:"syncedLyrics"`
}

// StatusError is an unexpected HTTP status from LRCLIB.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("lrclib: unexpected status %d", e.Code) }

// Permanent reports whether retrying soon is pointless (a 4xx other than
// 408 and 429).
func (e *StatusError) Permanent() bool {
	return e.Code >= 400 && e.Code < 500 && e.Code != http.StatusRequestTimeout && e.Code != http.StatusTooManyRequests
}

// RateLimitError is a 429 from LRCLIB.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("lrclib: rate limited, retry after %s", e.RetryAfter)
}

// Get looks up the exact record for a track (duration in seconds). A 404
// returns nil, nil.
func (c *Client) Get(ctx context.Context, track, artist, album string, durationSec int) (*Record, error) {
	q := url.Values{}
	q.Set("track_name", track)
	q.Set("artist_name", artist)
	q.Set("album_name", album)
	q.Set("duration", strconv.Itoa(durationSec))
	var rec Record
	found, err := c.getJSON(ctx, "/api/get?"+q.Encode(), &rec)
	if err != nil || !found || rec.ID == 0 {
		return nil, err // a 200 with no record ({} or null) is not found
	}
	return &rec, nil
}

// Search lists records matching a track and artist name.
func (c *Client) Search(ctx context.Context, track, artist string) ([]Record, error) {
	q := url.Values{}
	q.Set("track_name", track)
	q.Set("artist_name", artist)
	var recs []Record
	if _, err := c.getJSON(ctx, "/api/search?"+q.Encode(), &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// getJSON GETs path into v. It returns false (and no error) on 404.
func (c *Client) getJSON(ctx context.Context, path string, v any) (bool, error) {
	if err := c.lim.Wait(ctx); err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("lrclib: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := io.LimitReader(resp.Body, maxBody)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		_, _ = io.Copy(io.Discard, body)
		return false, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		_, _ = io.Copy(io.Discard, body)
		return false, &RateLimitError{RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	case resp.StatusCode != http.StatusOK:
		_, _ = io.Copy(io.Discard, body)
		return false, &StatusError{Code: resp.StatusCode}
	}
	if err := json.NewDecoder(body).Decode(v); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return false, fmt.Errorf("lrclib: response truncated or too large: %w", err)
		}
		return false, fmt.Errorf("lrclib: decode: %w", err)
	}
	return true, nil
}

// retryAfter parses a Retry-After value, in seconds or as an HTTP date
// (default 1m, at most 1h).
func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	var d time.Duration
	if n, err := strconv.Atoi(v); err == nil {
		d = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = time.Until(t).Round(time.Second)
	}
	if d <= 0 {
		return defaultRetry
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
