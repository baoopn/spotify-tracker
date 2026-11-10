package brandkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/baoopn/spotify-tracker/internal/logging"
)

const (
	defaultEvery   = 60 * time.Second
	defaultTimeout = 3 * time.Second
	maxBody        = 1 << 20
	userAgent      = "spotify-tracker-brandkit"
)

// mediaHosts are the only hosts a brand asset URL may point at.
var mediaHosts = map[string]bool{"media.baoopn.com": true, "baoopn.com": true}

// Asset is a validated brand file from the API.
type Asset struct {
	Slot        string
	URL         string
	ContentType string
	Width       int
	Height      int
	// Custom is true when the file was replaced in the baoopn admin.
	Custom bool
}

// Cache holds the last good copy of the brand API response.
type Cache struct {
	apiURL  string
	origin  *url.URL
	every   time.Duration
	timeout time.Duration
	client  *http.Client
	log     *slog.Logger
	assets  atomic.Pointer[map[string]Asset]

	// refreshing serializes Refresh so merges never interleave.
	refreshing sync.Mutex
	mu         sync.Mutex // guards the log state below
	// failing is true after a failed refresh until the next success.
	failing bool
	// ok is true after a successful refresh until the next failure.
	ok bool
	// rejected is the last rejected URL per slot (logged once each).
	rejected map[string]string
}

// New creates a cache of the brand API at apiURL, refreshed every `every`
// (60 s when <= 0). An empty apiURL disables the cache. An invalid one
// returns a disabled cache together with an error. New never returns nil.
func New(apiURL string, every time.Duration, client *http.Client, log *slog.Logger) (*Cache, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	if every <= 0 {
		every = defaultEvery
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	c := &Cache{every: every, timeout: defaultTimeout, client: client, log: log, rejected: map[string]string{}}
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		return c, nil
	}
	u, err := url.Parse(apiURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return c, fmt.Errorf("brandkit: BRAND_API_URL must be an absolute https URL without credentials")
	}
	c.apiURL = apiURL
	c.origin = &url.URL{Scheme: u.Scheme, Host: u.Host}
	return c, nil
}

// Enabled reports whether the cache polls the brand API.
func (c *Cache) Enabled() bool { return c != nil && c.apiURL != "" }

// Run refreshes the cache now and then every interval until ctx is done.
// It returns nil at once when the cache is disabled. Failures are logged and
// never stop it.
func (c *Cache) Run(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	t := time.NewTicker(c.every)
	defer t.Stop()
	for {
		_ = c.Refresh(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Refresh fetches the brand API once. On any error the cached copy is kept
// and the error is returned (and logged).
func (c *Cache) Refresh(ctx context.Context) error {
	if !c.Enabled() {
		return errors.New("brandkit: disabled")
	}
	c.refreshing.Lock()
	defer c.refreshing.Unlock()
	n, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		attrs := []any{logging.Err(err), slog.Bool("cached", c.assets.Load() != nil)}
		if c.failing {
			c.log.DebugContext(ctx, "brand API refresh failed; keeping last good copy", attrs...)
		} else {
			c.log.WarnContext(ctx, "brand API refresh failed; keeping last good copy", attrs...)
		}
		c.failing, c.ok = true, false
		return err
	}
	if !c.ok {
		c.log.InfoContext(ctx, "brand assets refreshed", slog.Int("assets", n))
	}
	c.failing, c.ok = false, true
	return nil
}

// Lookup returns the cached asset of a slot.
func (c *Cache) Lookup(slot string) (Asset, bool) {
	if c == nil {
		return Asset{}, false
	}
	m := c.assets.Load()
	if m == nil {
		return Asset{}, false
	}
	a, ok := (*m)[slot]
	return a, ok
}

// EmailLogo adapts the cache to notify.LogoFunc: the cached email_logo URL
// and size, if any.
func EmailLogo(c *Cache) func() (url string, width, height int, ok bool) {
	return func() (string, int, int, bool) {
		a, ok := c.Lookup(EmailLogoSlot)
		if !ok {
			return "", 0, 0, false
		}
		return a.URL, a.Width, a.Height, true
	}
}

// rawAsset is one entry of the API response.
type rawAsset struct {
	Slot        string `json:"slot"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Custom      bool   `json:"custom"`
}

type response struct {
	Data struct {
		Assets map[string]json.RawMessage `json:"assets"`
	} `json:"data"`
}

// fetch downloads and validates the response, merges it into the snapshot
// and returns the number of cached assets.
func (c *Cache) fetch(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("brandkit: brand API returned %s", resp.Status)
	}
	var body response
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return 0, fmt.Errorf("brandkit: decode brand API response: %w", err)
	}

	fresh := make(map[string]Asset, len(body.Data.Assets))
	for key, raw := range body.Data.Assets {
		if a, ok := c.validate(ctx, key, raw); ok {
			fresh[key] = a
		}
	}
	if len(fresh) == 0 {
		return 0, errors.New("brandkit: brand API response has no valid assets")
	}
	if prev := c.assets.Load(); prev != nil {
		for k, a := range *prev {
			if _, ok := fresh[k]; !ok {
				fresh[k] = a
			}
		}
	}
	c.assets.Store(&fresh)
	return len(fresh), nil
}

// validate checks one entry; rejected entries are logged once per URL.
func (c *Cache) validate(ctx context.Context, key string, raw json.RawMessage) (Asset, bool) {
	slot, known := SlotByName(key)
	if !known {
		c.log.DebugContext(ctx, "unknown brand slot skipped", slog.String("slot", key))
		return Asset{}, false
	}
	var r rawAsset
	if err := json.Unmarshal(raw, &r); err != nil {
		c.reject(ctx, key, "", "malformed entry")
		return Asset{}, false
	}
	if r.Slot != "" && r.Slot != key {
		c.reject(ctx, key, r.URL, "slot mismatch")
		return Asset{}, false
	}
	if !slot.Kind.accepts(r.ContentType) {
		c.reject(ctx, key, r.URL, "content type "+r.ContentType+" does not match the slot")
		return Asset{}, false
	}
	u, reason := c.resolve(r.URL)
	if reason != "" {
		c.reject(ctx, key, r.URL, reason)
		return Asset{}, false
	}
	c.accept(key)
	return Asset{Slot: key, URL: u, ContentType: r.ContentType,
		Width: max(r.Width, 0), Height: max(r.Height, 0), Custom: r.Custom}, true
}

// resolve returns the absolute media URL, or a reason it is not allowed.
func (c *Cache) resolve(raw string) (string, string) {
	if raw == "" {
		return "", "empty url"
	}
	if strings.ContainsAny(raw, " \t\r\n\"'<>\\`") {
		return "", "url contains unsafe characters"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "url does not parse"
	}
	if !u.IsAbs() {
		if !strings.HasPrefix(raw, "/media/") || strings.HasPrefix(raw, "//") {
			return "", "relative url outside /media/"
		}
		u = c.origin.ResolveReference(u)
	}
	switch {
	case u.Scheme != "https":
		return "", "url is not https"
	case u.User != nil:
		return "", "url has credentials"
	case u.Port() != "" && u.Port() != "443":
		return "", "url has a non-standard port"
	case !mediaHosts[strings.ToLower(u.Hostname())]:
		return "", "host is not allowed"
	}
	return u.String(), ""
}

func (c *Cache) reject(ctx context.Context, slot, u, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, seen := c.rejected[slot]; seen && prev == u {
		return
	}
	c.rejected[slot] = u
	c.log.WarnContext(ctx, "brand asset rejected", slog.String("slot", slot),
		slog.String("url", u), slog.String("reason", reason))
}

func (c *Cache) accept(slot string) {
	c.mu.Lock()
	delete(c.rejected, slot)
	c.mu.Unlock()
}
