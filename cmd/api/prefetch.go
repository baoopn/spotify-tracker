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

// Interfaces of the workers queue prefetch wakes (nil = not running).
type (
	upcomingLyrics interface {
		WakeUpcoming(trackIDs []string)
	}
	upcomingTranslator interface {
		EnqueueUpcoming(trackIDs []string)
	}
	waker interface {
		Wake()
	}
	lyricsWarmer interface {
		Warm(ctx context.Context, trackID string, force bool)
	}
)

// prefetchHooks are the workers queue prefetch wakes. Any may be nil.
// setOnFound and setOnDone chain onto the lyrics fetcher's and the
// translator's hooks (nil when that worker is off).
type prefetchHooks struct {
	lyrics     upcomingLyrics
	translator upcomingTranslator
	albums     waker
	images     waker
	warmer     lyricsWarmer
	setOnFound func(wrap func(prev func(string)) func(string))
	setOnDone  func(wrap func(prev func(string)) func(string))
}

// upcomingSource is the tracker side of queue prefetch.
type upcomingSource interface {
	SetPrefetchOptions(o tracker.PrefetchOptions)
	OnUpcoming(f func(trackIDs []string))
}

// hookPrefetch connects queue prefetch (tracks the poller found next in the
// Spotify queue) to the lyrics fetcher, the translator, the album and
// artist refreshers and the public lyrics cache.
//
// Lyrics lookups use the whole upcoming list; translation gets only the very
// next song (position 0), whatever QUEUE_PREFETCH_DEPTH is, so a deep queue
// does not spend Claude credit on songs far ahead.
func hookPrefetch(cfg config.API, svc upcomingSource, h prefetchHooks, log *slog.Logger) {
	svc.SetPrefetchOptions(tracker.PrefetchOptions{Enrich: cfg.Prefetch.Enrich})
	up := &upcomingSet{}
	warm := func(id string, force bool) {
		if h.warmer == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), warmTimeout)
			defer cancel()
			h.warmer.Warm(ctx, id, force)
		}()
	}
	// Lyrics just found, or a translation just stored, for an upcoming
	// track: (re)build its cached body.
	chain := func(force bool) func(prev func(string)) func(string) {
		return func(prev func(string)) func(string) {
			return func(id string) {
				if prev != nil {
					prev(id)
				}
				if up.has(id) {
					warm(id, force)
				}
			}
		}
	}
	if h.setOnFound != nil {
		h.setOnFound(chain(false))
	}
	if h.setOnDone != nil {
		h.setOnDone(chain(true))
	}
	doTranslate := cfg.Prefetch.Translate && h.translator != nil
	svc.OnUpcoming(func(ids []string) {
		changed := up.set(ids)
		if h.lyrics != nil {
			h.lyrics.WakeUpcoming(ids)
		}
		if doTranslate {
			h.translator.EnqueueUpcoming(ids[:min(1, len(ids))])
		}
		if len(ids) == 0 || !changed {
			return
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
			warm(id, false) // lyrics already stored: cached now
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

// set replaces the ids and reports whether they changed.
func (u *upcomingSet) set(ids []string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	changed := !slices.Equal(u.ids, ids)
	u.ids = slices.Clone(ids)
	return changed
}

func (u *upcomingSet) has(id string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Contains(u.ids, id)
}

// prefetchHooksFor builds the hooks from the concrete workers, leaving a
// field nil (not a typed nil) for a worker that is off.
func prefetchHooksFor(lr *lyrics.Refresher, tr *translate.Worker, albums *albumdetails.Refresher,
	images *artistimages.Refresher, warmer *httpapi.LyricsWarmer) prefetchHooks {
	var h prefetchHooks
	if lr != nil {
		h.lyrics = lr
		h.setOnFound = func(wrap func(func(string)) func(string)) { lr.OnFound = wrap(lr.OnFound) }
	}
	if tr != nil {
		h.translator = tr
		h.setOnDone = func(wrap func(func(string)) func(string)) { tr.OnDone = wrap(tr.OnDone) }
	}
	if albums != nil {
		h.albums = albums
	}
	if images != nil {
		h.images = images
	}
	if warmer != nil {
		h.warmer = warmer
	}
	return h
}
