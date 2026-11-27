// Package redisx creates the shared Redis client and holds the key names
// used across services, so every key lives in one place.
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/config"
)

// Key names and channels. Prefixed to allow sharing a Redis instance.
const (
	prefix = "spotify-tracker:"

	// KeyPlaybackState is the legacy hash {track_id, progress_ms} of the
	// play-count rules before the listen_state table. It is only read once
	// (when listen_state is empty) and then deleted; nothing writes it.
	KeyPlaybackState = prefix + "playback:state"
	// KeyUpNext holds the private "up next" list (domain.UpNext JSON) for the
	// admin overview: the next tracks of the user's Spotify queue.
	KeyUpNext = prefix + "playback:upnext"
	// KeyNowPlaying caches the JSON body of GET /api/v1/now-playing.
	KeyNowPlaying = prefix + "playback:now"
	// ChannelPlayback is the pub/sub channel for live now-playing updates.
	ChannelPlayback = prefix + "playback:events"
	// StreamEnrich is the stream of tracks that need enrichment.
	StreamEnrich = prefix + "enrich:requests"
	// GroupEnrich is the consumer group used by enricher workers.
	GroupEnrich = "enricher"
	// KeySpotifyAccessToken caches the Spotify access token.
	KeySpotifyAccessToken = prefix + "spotify:access-token"
	// KeySpotifyRefreshToken stores a rotated refresh token, if Spotify issued one.
	KeySpotifyRefreshToken = prefix + "spotify:refresh-token"
	// KeyCachePrefix prefixes HTTP response cache entries.
	KeyCachePrefix = prefix + "cache:"
	// KeyCacheGeneration is part of every response-cache key; bumping it
	// (BumpCache) makes all cached responses stale at once.
	KeyCacheGeneration = prefix + "cache-gen"
	// KeyListenAudioPrefix prefixes the listen-along audio lookup cache:
	// which file extension a track's audio has in S3, or that it has none.
	KeyListenAudioPrefix = prefix + "listen:audio:"
	// KeyListenAudioSyncLock is held (SET NX, 10 min) while one replica lists
	// the audio bucket; KeyListenAudioSyncStatus is the last sync result and
	// KeyListenAudioSyncCooldown limits syncs started from the admin.
	KeyListenAudioSyncLock     = prefix + "listen:audio-sync:lock"
	KeyListenAudioSyncStatus   = prefix + "listen:audio-sync:status"
	KeyListenAudioSyncCooldown = prefix + "listen:audio-sync:cooldown"
	// KeyListenRequestRatePrefix counts "Request an invite" submissions per
	// salted client IP hash and hour (fixed window).
	KeyListenRequestRatePrefix = prefix + "listen:requests:rate:"
	// KeyListenPresencePrefix prefixes who is on /listen right now, one key
	// per listener ID (JSON, 90 s TTL).
	KeyListenPresencePrefix = prefix + "listen:presence:"
)

// BumpCache invalidates every cached API response by moving to a new cache
// generation. Old entries simply expire with their TTL.
func BumpCache(ctx context.Context, rdb *redis.Client) error {
	return rdb.Incr(ctx, KeyCacheGeneration).Err()
}

// KeyPlaylistLock guards a single in-flight playlist metadata fetch.
func KeyPlaylistLock(id string) string { return prefix + "playlist:fetching:" + id }

// KeyCatalogSeen marks a track whose catalog rows were recently upserted.
func KeyCatalogSeen(trackID string) string { return prefix + "catalog:seen:" + trackID }

// Connect parses the URL, creates a client and pings it.
func Connect(ctx context.Context, cfg config.Redis) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("redis: parse url: %w", err)
	}
	opts.ClientName = "spotify-tracker"
	client := redis.NewClient(opts)

	var pingErr error
	for attempt := 1; attempt <= 10; attempt++ {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		pingErr = client.Ping(pctx).Err()
		cancel()
		if pingErr == nil {
			return client, nil
		}
		select {
		case <-ctx.Done():
			_ = client.Close()
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
	}
	_ = client.Close()
	return nil, fmt.Errorf("redis: ping: %w", pingErr)
}
