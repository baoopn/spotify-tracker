package config

import (
	"fmt"
	"time"
)

// QueuePrefetch configures how the poller reads the user's Spotify queue
// and asks the API to prefetch the next tracks (catalog rows, lyrics,
// translation, cache) before they play.
type QueuePrefetch struct {
	// Enabled reads GET /me/player/queue while something plays.
	Enabled bool `env:"QUEUE_PREFETCH_ENABLED" envDefault:"true"`
	// Depth is how many upcoming tracks are prefetched (1..5).
	Depth int `env:"QUEUE_PREFETCH_DEPTH" envDefault:"1"`
	// Recheck re-reads the queue this often while the same song plays, to
	// catch queue edits (15s..10m). A track change always re-reads it.
	Recheck time.Duration `env:"QUEUE_PREFETCH_RECHECK" envDefault:"60s"`
	// TTL is how long a prefetched track is not prefetched again (1m..48h).
	TTL time.Duration `env:"QUEUE_PREFETCH_TTL" envDefault:"6h"`
}

func (q *QueuePrefetch) validate() error {
	if !q.Enabled {
		return nil
	}
	switch {
	case q.Depth < 1 || q.Depth > 5:
		return fmt.Errorf("QUEUE_PREFETCH_DEPTH must be in 1..5")
	case q.Recheck < 15*time.Second || q.Recheck > 10*time.Minute:
		return fmt.Errorf("QUEUE_PREFETCH_RECHECK must be between 15s and 10m")
	case q.TTL < time.Minute || q.TTL > 48*time.Hour:
		return fmt.Errorf("QUEUE_PREFETCH_TTL must be between 1m and 48h")
	}
	return nil
}

// Prefetch configures what the API does for upcoming tracks the poller
// reports.
type Prefetch struct {
	// Translate queues upcoming tracks for Claude translation (just below
	// the song playing now). No effect when TRANSLATE_ENABLED=false; the
	// translation caps still apply. A skipped song still costs a call.
	Translate bool `env:"QUEUE_PREFETCH_TRANSLATE" envDefault:"true"`
	// Enrich queues Soundcharts enrichment for upcoming tracks (uses quota);
	// off, they are enriched when they play.
	Enrich bool `env:"QUEUE_PREFETCH_ENRICH" envDefault:"false"`
	// Details wakes the album details and artist image refreshers, which
	// call the Spotify API, for upcoming tracks.
	Details bool `env:"QUEUE_PREFETCH_DETAILS" envDefault:"true"`
}
