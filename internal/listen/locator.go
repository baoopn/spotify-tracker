package listen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// AudioDB reads and records audio files (implemented by *store.Store).
type AudioDB interface {
	TrackAudioFor(ctx context.Context, trackID string) (store.TrackAudio, error)
	UpsertTrackAudio(ctx context.Context, a store.TrackAudio) error
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
	// db is the track_audio table (nil: HEAD lookups only).
	db AudioDB
}

// NewLocator builds a Locator, or nil when S3 is not configured. db may be
// nil (every lookup then asks the bucket).
func NewLocator(cfg config.Listen, rdb *redis.Client, db AudioDB, log *slog.Logger) *Locator {
	s := NewSigner(cfg.S3)
	if s == nil {
		return nil
	}
	return &Locator{signer: s, prefix: cfg.Prefix, exts: cfg.AudioExtensions, ttl: cfg.AudioURLTTL,
		cache: redisCache{rdb}, client: &http.Client{Timeout: 5 * time.Second}, log: log, now: time.Now, db: db}
}

// Locate returns a presigned URL for the audio of trackID, valid for the
// configured TTL or until remaining (the rest of the song) plus a minute,
// whichever is longer, at most an hour. A file recorded in track_audio is
// signed at once, with no request to the bucket; otherwise the bucket is
// asked with HEAD requests (cached in Redis), and a found file is recorded.
// It returns nil, nil when there is no file (or trackID is not a Spotify
// ID), and an error when the bucket could not answer. Errors and logs never
// contain a presigned URL.
func (l *Locator) Locate(ctx context.Context, trackID string, remaining time.Duration) (*AudioURL, error) {
	if !spotifyID.MatchString(trackID) {
		return nil, nil
	}
	if l.db != nil {
		a, err := l.db.TrackAudioFor(ctx, trackID)
		switch {
		case err == nil:
			ctype := ""
			if a.ContentType != nil {
				ctype = *a.ContentType
			}
			return l.sign(trackID, cached{Ext: a.Ext, Type: audioType(ctype, a.Ext)}, remaining), nil
		case !errors.Is(err, domain.ErrNotFound):
			l.log.WarnContext(ctx, "audio lookup: reading track_audio failed; asking the bucket", logging.Err(err))
		}
	}
	c, err := l.find(ctx, trackID)
	if err != nil || c.Ext == "" {
		return nil, err
	}
	return l.sign(trackID, c, remaining), nil
}

// Find reports whether the bucket has an audio file for trackID and its
// extension, from the Redis lookup cache or HEAD requests (a found file is
// recorded in track_audio). It skips track_audio itself and never signs a
// URL. An error means the bucket could not answer.
func (l *Locator) Find(ctx context.Context, trackID string) (ext string, found bool, err error) {
	if !spotifyID.MatchString(trackID) {
		return "", false, nil
	}
	c, err := l.find(ctx, trackID)
	return c.Ext, c.Ext != "", err
}

// find reads the lookup cache, else asks the bucket once for all callers.
func (l *Locator) find(ctx context.Context, trackID string) (cached, error) {
	key := redisx.KeyListenAudioPrefix + trackID
	var c cached
	raw, err := l.cache.get(ctx, key)
	if err == nil && json.Unmarshal(raw, &c) == nil {
		return c, nil
	}
	v, err, _ := l.sf.Do(trackID, func() (any, error) { return l.lookup(ctx, trackID, key) })
	if err != nil {
		return cached{}, err
	}
	return v.(cached), nil
}

// sign presigns the GET URL of a found file.
func (l *Locator) sign(trackID string, c cached, remaining time.Duration) *AudioURL {
	ttl := min(max(l.ttl, remaining+urlGrace), maxURLTTL)
	now := l.now()
	return &AudioURL{
		URL:         l.signer.Presign(http.MethodGet, l.prefix+trackID+"."+c.Ext, ttl, now),
		ExpiresAt:   now.Add(ttl).UTC().Truncate(time.Second),
		ContentType: c.Type,
	}
}

// lookup sends a HEAD request per extension, in order, and caches the
// answer. It runs once for all concurrent callers, so it does not use the
// caller's cancellation.
func (l *Locator) lookup(parent context.Context, trackID, key string) (cached, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), lookupTimeout)
	defer cancel()
	forbidden := 0
	for _, ext := range l.exts {
		h, err := l.head(ctx, l.prefix+trackID+"."+ext)
		if err != nil {
			return cached{}, err
		}
		switch h.status {
		case http.StatusOK:
			c := cached{Ext: ext, Type: audioType(h.ctype, ext)}
			l.store(ctx, key, c, foundTTL)
			l.record(ctx, trackID, c, h)
			return c, nil
		case http.StatusNotFound:
		case http.StatusForbidden:
			forbidden++
		default:
			return cached{}, fmt.Errorf("audio lookup: storage answered %d", h.status)
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

// record stores a file found by HEAD in track_audio, so the next lookup
// needs no request. Failures are logged only.
func (l *Locator) record(ctx context.Context, trackID string, c cached, h headResult) {
	if l.db == nil {
		return
	}
	ctype := c.Type
	a := store.TrackAudio{TrackID: trackID, Ext: c.Ext, SizeBytes: max(0, h.size), ContentType: &ctype}
	if h.etag != "" {
		etag := h.etag
		a.ETag = &etag
	}
	if t, err := http.ParseTime(h.lastModified); err == nil {
		a.LastModified = &t
	}
	if err := l.db.UpsertTrackAudio(ctx, a); err != nil {
		l.log.WarnContext(ctx, "audio lookup: recording the file failed", logging.Err(err))
	}
}

type headResult struct {
	status       int
	ctype, etag  string
	lastModified string
	size         int64
}

// head sends one presigned HEAD request. The error never includes the URL.
func (l *Locator) head(ctx context.Context, objectKey string) (headResult, error) {
	ctx, cancel := context.WithTimeout(ctx, headTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		l.signer.Presign(http.MethodHead, objectKey, headTTL, l.now()), nil)
	if err != nil {
		return headResult{}, errors.New("audio lookup: bad storage URL")
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return headResult{}, fmt.Errorf("audio lookup: %w", stripURL(err))
	}
	_ = resp.Body.Close()
	return headResult{status: resp.StatusCode, ctype: resp.Header.Get("Content-Type"), etag: resp.Header.Get("ETag"),
		lastModified: resp.Header.Get("Last-Modified"), size: resp.ContentLength}, nil
}

// stripURL drops the (presigned) request URL from an HTTP client error.
func stripURL(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err
	}
	return err
}
