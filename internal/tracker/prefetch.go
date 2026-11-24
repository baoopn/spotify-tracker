package tracker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// upNextTTL is how long the "up next" list lives without a refresh (the
// poller re-reads the queue every minute or so while a song plays).
const upNextTTL = 5 * time.Minute

// PrefetchOptions tune what prefetching an upcoming track does.
type PrefetchOptions struct {
	// Enrich also queues Soundcharts enrichment (uses quota). Off, the
	// track is enriched when it plays.
	Enrich bool
}

// SetPrefetchOptions sets the prefetch options.
func (s *Service) SetPrefetchOptions(o PrefetchOptions) { s.prefetch = o }

// OnUpcoming sets a hook called with the track ids that were just
// prefetched (lyrics, translation, metadata lookups and the lyrics cache
// warm up on it). It must not block.
func (s *Service) OnUpcoming(f func(trackIDs []string)) { s.onUpcoming = f }

// PrefetchUpcoming stores the catalog rows of the next tracks of the user's
// queue and replaces the private "up next" list. It never records a play:
// no history, replay, listen state, current playback or response-cache
// change. Items with Prefetch false are only listed. It returns the ids it
// stored.
//
// The catalog-seen marker is deliberately not set (unless enrichment runs
// now): the real play must still create the enrichment row and queue it.
func (s *Service) PrefetchUpcoming(ctx context.Context, current string, items []domain.Upcoming, at time.Time) ([]string, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	stored := []string{}
	for _, it := range items {
		if !it.Prefetch || it.Track.ID == "" || it.Track.ID == current {
			continue
		}
		enrich := false
		err := s.store.InTx(ctx, func(q *store.Queries) error {
			if err := upsertCatalogEntry(ctx, q, it.CatalogEntry); err != nil {
				return err
			}
			if !s.prefetch.Enrich {
				return nil
			}
			var err error
			enrich, err = q.EnsureTrackFeatures(ctx, it.Track.ID, it.Track.ISRC)
			return err
		})
		if err != nil {
			if ctx.Err() != nil {
				return stored, ctx.Err()
			}
			s.log.WarnContext(ctx, "prefetch track failed", slog.String("track_id", it.Track.ID), logging.Err(err))
			continue
		}
		if s.prefetch.Enrich {
			if enrich {
				s.queueEnrichment(ctx, it.Track.ID, it.Track.ISRC)
			}
			_ = s.rdb.Set(ctx, redisx.KeyCatalogSeen(it.Track.ID), 1, catalogSeenTTL).Err()
		}
		stored = append(stored, it.Track.ID)
	}
	s.saveUpNext(ctx, current, items, at)
	if len(stored) > 0 && s.onUpcoming != nil {
		s.onUpcoming(stored)
	}
	return stored, nil
}

// saveUpNext replaces the "up next" list; an empty list removes it.
func (s *Service) saveUpNext(ctx context.Context, current string, items []domain.Upcoming, at time.Time) {
	if len(items) == 0 {
		if err := s.rdb.Del(ctx, redisx.KeyUpNext).Err(); err != nil {
			s.log.WarnContext(ctx, "clear up next failed", logging.Err(err))
		}
		return
	}
	body, err := json.Marshal(upNextFrom(current, items, at))
	if err != nil {
		return
	}
	if err := s.rdb.Set(ctx, redisx.KeyUpNext, body, upNextTTL).Err(); err != nil {
		s.log.WarnContext(ctx, "save up next failed", logging.Err(err))
	}
}

// clearUpNext removes the "up next" list (nothing is playing).
func (s *Service) clearUpNext(ctx context.Context) {
	if err := s.rdb.Del(ctx, redisx.KeyUpNext).Err(); err != nil {
		s.log.WarnContext(ctx, "clear up next failed", logging.Err(err))
	}
}

// upNextFrom builds the "up next" list from the queue entries.
func upNextFrom(current string, items []domain.Upcoming, at time.Time) domain.UpNext {
	up := domain.UpNext{FetchedAt: at.UTC(), ForTrackID: current, Items: make([]domain.UpNextItem, 0, len(items))}
	for _, it := range items {
		item := domain.UpNextItem{
			Position: it.Position, TrackID: it.Track.ID, Name: it.Track.Name, DurationMS: it.Track.DurationMS,
			Artists: make([]domain.UpNextName, 0, len(it.Artists)), AlbumName: it.Album.Name,
			CoverImageURL: it.Album.CoverImageURL,
		}
		for _, a := range it.Artists {
			item.Artists = append(item.Artists, domain.UpNextName{ID: a.ID, Name: a.Name})
		}
		up.Items = append(up.Items, item)
	}
	return up
}
