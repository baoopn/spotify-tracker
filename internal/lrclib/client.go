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
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// DefaultBaseURL is the public LRCLIB instance.
const DefaultBaseURL = "https://lrclib.net"

// UserAgent identifies the client to LRCLIB.
const UserAgent = "spotify-tracker (https://github.com/baoopn/spotify-tracker)"

const (
	maxBody        = 4 << 20 // response bodies are read up to 4 MiB
	maxText        = 200_000 // stored lyrics are capped at this many bytes
	durationSlack  = 2.0     // seconds a search candidate may differ from the track
	defaultRetry   = time.Minute
	maxRetryAfter  = time.Hour
	defaultTimeout = 20 * time.Second
)

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
		hc = &http.Client{Timeout: defaultTimeout}
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

// Lookup finds the lyrics of a track: the exact match first, then the best
// search candidate within 2 s of the track's duration. nil, nil means no
// lyrics were found.
func (c *Client) Lookup(ctx context.Context, cand domain.LyricsCandidate) (*domain.Lyrics, error) {
	durSec := int(math.Round(float64(cand.DurationMS) / 1000))
	if cand.Album != "" && durSec > 0 {
		rec, err := c.Get(ctx, cand.Name, cand.Artist, cand.Album, durSec)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			return toLyrics(rec), nil
		}
	}
	recs, err := c.Search(ctx, cand.Name, cand.Artist)
	if err != nil {
		return nil, err
	}
	if rec := pick(recs, cand.Name, cand.Artist, durSec); rec != nil {
		return toLyrics(rec), nil
	}
	return nil, nil
}

// pick chooses the best search candidate. A candidate must have the same
// track name (folded, ignoring a bracketed or " - " version suffix such as
// "(Live)" or "- Remastered"), an artist name that contains the primary
// artist or is contained in it (folded), and, when the duration is known, a
// duration within durationSlack. Among those it prefers the exact folded
// name, then synced lyrics, then plain lyrics, then the closest duration.
func pick(recs []Record, name, artist string, durSec int) *Record {
	want, wantBase, wantArtist := fold(name), baseName(name), fold(artist)
	var best *Record
	var bestKey [4]float64
	for i := range recs {
		r := &recs[i]
		if r.ID == 0 || baseName(r.TrackName) != wantBase || !artistMatch(fold(r.ArtistName), wantArtist) {
			continue
		}
		diff := 0.0
		if durSec > 0 {
			if diff = math.Abs(r.Duration - float64(durSec)); diff > durationSlack {
				continue
			}
		}
		key := [4]float64{b2f(fold(r.TrackName) == want), b2f(nonBlank(r.SyncedLyrics)), b2f(nonBlank(r.PlainLyrics)), -diff}
		if best == nil || less(bestKey, key) {
			best, bestKey = r, key
		}
	}
	return best
}

// nameSuffix matches a trailing "(…)", "[…]" or " - …" version suffix.
var nameSuffix = regexp.MustCompile(`\s*(?:\([^)]*\)|\[[^\]]*\]|\s-\s.*)$`)

// baseName folds a track name and removes version suffixes ("(Live)",
// "[Remix]", "- Remastered 2011").
func baseName(s string) string {
	b := fold(s)
	for {
		next := strings.TrimSpace(nameSuffix.ReplaceAllString(b, ""))
		if next == b || next == "" {
			return b
		}
		b = next
	}
}

// artistMatch reports whether one folded artist name contains the other.
func artistMatch(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return strings.Contains(got, want) || strings.Contains(want, got)
}

func less(a, b [4]float64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func nonBlank(s *string) bool { return s != nil && strings.TrimSpace(*s) != "" }

var lrcStamp = regexp.MustCompile(`^\[\d+:\d+(?:\.\d+)?\]\s*`)

// cleanText removes NUL bytes (Postgres text rejects them), normalizes line
// breaks to "\n" and trims the text.
func cleanText(s *string) string {
	if s == nil {
		return ""
	}
	t := strings.ReplaceAll(*s, "\x00", "")
	t = strings.ReplaceAll(t, "\r\n", "\n")
	t = strings.ReplaceAll(t, "\r", "\n")
	return strings.TrimSpace(t)
}

// toLyrics maps a record. It is instrumental when flagged or when no lyrics
// text is left; plain text is derived from synced text when missing.
func toLyrics(r *Record) *domain.Lyrics {
	plain, synced := cleanText(r.PlainLyrics), cleanText(r.SyncedLyrics)
	if plain == "" && synced != "" {
		lines := strings.Split(synced, "\n")
		for i, l := range lines {
			lines[i] = lrcStamp.ReplaceAllString(l, "")
		}
		plain = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	if r.Instrumental || plain == "" {
		return &domain.Lyrics{Status: domain.LyricsInstrumental, LRCLibID: r.ID}
	}
	return &domain.Lyrics{Status: domain.LyricsFound, LRCLibID: r.ID, Plain: capText(plain), Synced: capText(synced)}
}

// capText cuts s to at most maxText bytes, at the last newline before the cap.
func capText(s string) string {
	if len(s) <= maxText {
		return s
	}
	cut := s[:maxText]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		return cut[:i]
	}
	// No newline: cut at a rune boundary.
	for i := maxText; i > 0; i-- {
		if (s[i] & 0xC0) != 0x80 {
			return s[:i]
		}
	}
	return ""
}

// foldTable maps accented lowercase letters (Vietnamese and common Latin)
// to their base letter.
var foldTable = func() map[rune]rune {
	m := map[rune]rune{}
	for base, set := range map[rune]string{
		'a': "àáảãạăằắẳẵặâầấẩẫậäåā", 'e': "èéẻẽẹêềếểễệëē", 'i': "ìíỉĩịïî",
		'o': "òóỏõọôồốổỗộơờớởỡợöøō", 'u': "ùúủũụưừứửữựüûū", 'y': "ỳýỷỹỵÿ",
		'd': "đð", 'c': "ç", 'n': "ñ", 'l': "ł",
	} {
		for _, r := range set {
			m[r] = base
		}
	}
	return m
}()

// fold lowercases s and removes accents, close to search_fold in SQL; it is
// only used to compare track names.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if r >= 0x300 && r <= 0x36f { // combining marks of decomposed text
			continue
		}
		if f, ok := foldTable[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
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
