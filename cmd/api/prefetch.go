package main

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/baoopn/spotify-tracker/internal/albumdetails"
	"github.com/baoopn/spotify-tracker/internal/artistimages"
	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/httpapi"
	"github.com/baoopn/spotify-tracker/internal/lyrics"
	"github.com/baoopn/spotify-tracker/internal/tracker"
	"github.com/baoopn/spotify-tracker/internal/translate"
)

// warmTimeout bounds one lyrics cache warm-up.
const warmTimeout = 5 * time.Second

// prefetchHooks are the workers queue prefetch wakes. Any may be nil.
type prefetchHooks struct {
	lyrics     *lyrics.Refresher
	translator *translate.Worker
	albums     *albumdetails.Refresher
	images     *artistimages.Refresher
	warmer     *httpapi.LyricsWarmer
}

// hookPrefetch connects queue prefetch (tracks the poller found next in the
// Spotify queue) to the lyrics fetcher, the translator, the album and
// artist refreshers and the public lyrics cache.
func hookPrefetch(cfg config.API, svc *tracker.Service, h prefetchHooks, log *slog.Logger) {
	svc.SetPrefetchOptions(tracker.PrefetchOptions{Enrich: cfg.Prefetch.Enrich})
	up := &upcomingSet{}
	warm := func(id string) {
		if h.warmer == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), warmTimeout)
			defer cancel()
			h.warmer.Warm(ctx, id)
		}()
	}
	if h.lyrics != nil {
		prev := h.lyrics.OnFound
		h.lyrics.OnFound = func(id string) {
			if prev != nil {
				prev(id)
			}
			if up.has(id) {
				warm(id) // lyrics just arrived for an upcoming track
			}
		}
	}
	doTranslate := cfg.Prefetch.Translate && h.translator != nil
	svc.OnUpcoming(func(ids []string) {
		up.set(ids)
		if h.lyrics != nil {
			h.lyrics.WakeUpcoming(ids)
		}
		if doTranslate {
			h.translator.EnqueueUpcoming(ids)
		}
		if cfg.Prefetch.Details {
			if h.albums != nil {
				h.albums.Wake()
			}
			if h.images != nil {
				h.images.Wake()
			}
		}
		for _, id := range ids {
			warm(id) // lyrics already stored: cached now
		}
	})
	log.Info("queue prefetch hooks ready", slog.Bool("translate", doTranslate),
		slog.Bool("enrich", cfg.Prefetch.Enrich), slog.Bool("details", cfg.Prefetch.Details))
}

// upcomingSet holds the latest upcoming track ids.
type upcomingSet struct {
	mu  sync.Mutex
	ids []string
}

func (u *upcomingSet) set(ids []string) {
	u.mu.Lock()
	u.ids = append([]string(nil), ids...)
	u.mu.Unlock()
}

func (u *upcomingSet) has(id string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Contains(u.ids, id)
}
