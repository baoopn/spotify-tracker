package listen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
)

// AudioURL is a presigned URL for the audio of one track.
type AudioURL struct {
	URL         string
	ExpiresAt   time.Time
	ContentType string
}

const (
	foundTTL      = time.Hour       // a found file's extension is cached this long
	missingTTL    = 5 * time.Minute // "no file" is cached this long
	headTTL       = time.Minute     // lifetime of a HEAD probe URL
	headTimeout   = 2 * time.Second // per HEAD request
	lookupTimeout = 6 * time.Second // all HEAD requests of one lookup
	maxURLTTL     = time.Hour
	urlGrace      = time.Minute // a URL outlives the end of the song by this much
)

var spotifyID = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

// extTypes are the audio types by file extension, used when the bucket
// does not return an audio/* Content-Type.
var extTypes = map[string]string{
	"mp3": "audio/mpeg", "m4a": "audio/mp4", "aac": "audio/aac", "flac": "audio/flac",
	"ogg": "audio/ogg", "opus": "audio/ogg", "webm": "audio/webm", "wav": "audio/wav",
}

// cached is the Redis value of a lookup: Ext is empty when there is no file.
type cached struct {
	Ext  string `json:"ext,omitempty"`
	Type string `json:"type,omitempty"`
}

// lookupCache stores lookups (Redis; tests replace it).
type lookupCache interface {
	get(ctx context.Context, key string) ([]byte, error)
	set(ctx context.Context, key string, raw []byte, ttl time.Duration)
}

type redisCache struct{ rdb *redis.Client }

func (c redisCache) get(ctx context.Context, key string) ([]byte, error) {
	return c.rdb.Get(ctx, key).Bytes()
}

func (c redisCache) set(ctx context.Context, key string, raw []byte, ttl time.Duration) {
	_ = c.rdb.Set(ctx, key, raw, ttl).Err()
}

// Locator finds the audio file of a track ({prefix}{track_id}.{ext}) and
// presigns a GET URL for it.
type Locator struct {
	signer *Signer
	prefix string
	exts   []string
	ttl    time.Duration
	cache  lookupCache
	client *http.Client
	sf     singleflight.Group
	log    *slog.Logger
	now    func() time.Time
}

// NewLocator builds a Locator, or nil when S3 is not configured.
func NewLocator(cfg config.Listen, rdb *redis.Client, log *slog.Logger) *Locator {
	s := NewSigner(cfg.S3)
	if s == nil {
		return nil
	}
	return &Locator{signer: s, prefix: cfg.Prefix, exts: cfg.AudioExtensions, ttl: cfg.AudioURLTTL,
		cache: redisCache{rdb}, client: &http.Client{Timeout: 5 * time.Second}, log: log, now: time.Now}
}

// Locate returns a presigned URL for the audio of trackID, valid for the
// configured TTL or until remaining (the rest of the song) plus a minute,
// whichever is longer, at most an hour. It returns nil, nil when there is no
// file (or trackID is not a Spotify ID), and an error when the bucket could
// not answer. Errors and logs never contain a presigned URL.
func (l *Locator) Locate(ctx context.Context, trackID string, remaining time.Duration) (*AudioURL, error) {
	if !spotifyID.MatchString(trackID) {
		return nil, nil
	}
	key := redisx.KeyListenAudioPrefix + trackID
	var c cached
	raw, err := l.cache.get(ctx, key)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		v, err, _ := l.sf.Do(trackID, func() (any, error) { return l.lookup(ctx, trackID, key) })
		if err != nil {
			return nil, err
		}
		c = v.(cached)
	}
	if c.Ext == "" {
		return nil, nil
	}
	ttl := min(max(l.ttl, remaining+urlGrace), maxURLTTL)
	now := l.now()
	return &AudioURL{
		URL:         l.signer.Presign(http.MethodGet, l.prefix+trackID+"."+c.Ext, ttl, now),
		ExpiresAt:   now.Add(ttl).UTC().Truncate(time.Second),
		ContentType: c.Type,
	}, nil
}

// lookup sends a HEAD request per extension, in order, and caches the
// answer. It runs once for all concurrent callers, so it does not use the
// caller's cancellation.
func (l *Locator) lookup(parent context.Context, trackID, key string) (cached, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), lookupTimeout)
	defer cancel()
	forbidden := 0
	for _, ext := range l.exts {
		status, ctype, err := l.head(ctx, l.prefix+trackID+"."+ext)
		if err != nil {
			return cached{}, err
		}
		switch status {
		case http.StatusOK:
			c := cached{Ext: ext, Type: audioType(ctype, ext)}
			l.store(ctx, key, c, foundTTL)
			return c, nil
		case http.StatusNotFound:
		case http.StatusForbidden:
			forbidden++
		default:
			return cached{}, fmt.Errorf("audio lookup: storage answered %d", status)
		}
	}
	if forbidden == len(l.exts) {
		// S3 answers 403 for a missing key when the credentials may not list
		// the bucket, and for bad credentials.
		l.log.WarnContext(ctx, "audio lookup: storage refused every request; check S3 credentials / ListBucket permission")
	}
	l.store(ctx, key, cached{}, missingTTL)
	return cached{}, nil
}

func (l *Locator) store(ctx context.Context, key string, c cached, ttl time.Duration) {
	if raw, err := json.Marshal(c); err == nil {
		l.cache.set(ctx, key, raw, ttl)
	}
}

// head sends one presigned HEAD request. The error never includes the URL.
func (l *Locator) head(ctx context.Context, objectKey string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, headTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		l.signer.Presign(http.MethodHead, objectKey, headTTL, l.now()), nil)
	if err != nil {
		return 0, "", errors.New("audio lookup: bad storage URL")
	}
	resp, err := l.client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err // drop the presigned URL from the message
		}
		return 0, "", fmt.Errorf("audio lookup: %w", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Content-Type"), nil
}

// audioType is the bucket's Content-Type when it is audio/*, else the type
// for the extension.
func audioType(header, ext string) string {
	if mt, _, err := mime.ParseMediaType(header); err == nil && strings.HasPrefix(mt, "audio/") {
		return mt
	}
	if t, ok := extTypes[ext]; ok {
		return t
	}
	return "application/octet-stream"
}
