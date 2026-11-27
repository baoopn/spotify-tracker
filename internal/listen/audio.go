package listen

import (
	"context"
	"mime"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
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
