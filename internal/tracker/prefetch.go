package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

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

// OnUpcoming sets a hook called with the upcoming track ids in queue order
// (the song playing now excluded) whenever the poller reports the queue, and
// with nil when playback goes idle. Lyrics, translation and metadata lookups
// and the lyrics cache key off it. It must not block.
func (s *Service) OnUpcoming(f func(trackIDs []string)) { s.onUpcoming = f }

// prefetchDeps is what PrefetchUpcoming needs (an interface so tests can
// check it never records a play or marks the catalog as seen).
type prefetchDeps interface {
	// UpsertEntry writes the catalog rows; with enrich it also ensures the
	// enrichment row and reports whether to queue it.
	UpsertEntry(ctx context.Context, e domain.CatalogEntry, enrich bool) (bool, error)
	CurrentTrack(ctx context.Context) (string, bool, error)
	SetUpNext(ctx context.Context, body []byte) error
	ClearUpNext(ctx context.Context) error
	MarkCatalogSeen(ctx context.Context, trackID string)
	QueueEnrichment(ctx context.Context, trackID, isrc string)
}

// PrefetchUpcoming stores the catalog rows of the next tracks of the user's
// queue and replaces the private "up next" list. It never records a play:
// no history, replay, listen state, current playback or response-cache
// change. Items with Prefetch false are only listed. It returns the ids it
// stored.
//
// The catalog-seen marker is deliberately not set (unless enrichment runs
// now): the real play must still create the enrichment row and queue it.
func (s *Service) PrefetchUpcoming(ctx context.Context, current string, items []domain.Upcoming, at time.Time) ([]string, error) {
	return prefetchUpcoming(ctx, serviceDeps{s}, s.prefetch, s.log, s.onUpcoming, current, items, at)
}

func prefetchUpcoming(ctx context.Context, d prefetchDeps, o PrefetchOptions, log *slog.Logger,
	hook func([]string), current string, items []domain.Upcoming, at time.Time) ([]string, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	items = dedupeUpcoming(current, items)
	stored := []string{}
	for _, it := range items {
		if !it.Prefetch {
			continue
		}
		enrich, err := d.UpsertEntry(ctx, it.CatalogEntry, o.Enrich)
		if err != nil {
			if ctx.Err() != nil {
				return stored, ctx.Err()
			}
			log.WarnContext(ctx, "prefetch track failed", slog.String("track_id", it.Track.ID), logging.Err(err))
			continue
		}
		if o.Enrich {
			if enrich {
				d.QueueEnrichment(ctx, it.Track.ID, it.Track.ISRC)
			}
			d.MarkCatalogSeen(ctx, it.Track.ID)
		}
		stored = append(stored, it.Track.ID)
	}

	// Playback may have stopped (or moved on) while the poller read the
	// queue: then the list is stale and must not be shown or acted on.
	if cur, playing, err := d.CurrentTrack(ctx); err != nil || !playing || cur != current {
		return stored, nil
	}
	if len(items) == 0 {
		// The queue emptied while the song plays: clear the list and the
		// lookups keyed off it (lyrics, translation, cache warm-up).
		if err := d.ClearUpNext(ctx); err != nil {
			log.WarnContext(ctx, "clear up next failed", logging.Err(err))
		}
		if hook != nil {
			hook(nil)
		}
		return stored, nil
	}
	if body, err := json.Marshal(upNextFrom(current, items, at)); err == nil {
		if err := d.SetUpNext(ctx, body); err != nil {
			log.WarnContext(ctx, "save up next failed", logging.Err(err))
		}
	}
	if hook != nil {
		ids := make([]string, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.Track.ID)
		}
		hook(ids)
	}
	return stored, nil
}

// dedupeUpcoming drops entries without an id, the song playing now and
// repeats, and renumbers positions in queue order.
func dedupeUpcoming(current string, items []domain.Upcoming) []domain.Upcoming {
	out := make([]domain.Upcoming, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		if it.Track.ID == "" || it.Track.ID == current || seen[it.Track.ID] {
			continue
		}
		seen[it.Track.ID] = true
		it.Position = int32(len(out))
		out = append(out, it)
	}
	return out
}

// clearUpcoming removes the "up next" list and tells the hooks nothing is
// upcoming (nothing is playing).
func (s *Service) clearUpcoming(ctx context.Context) {
	if err := (serviceDeps{s}).ClearUpNext(ctx); err != nil {
		s.log.WarnContext(ctx, "clear up next failed", logging.Err(err))
	}
	if s.onUpcoming != nil {
		s.onUpcoming(nil)
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

// serviceDeps implements prefetchDeps with the service's store and Redis.
type serviceDeps struct{ s *Service }

func (d serviceDeps) UpsertEntry(ctx context.Context, e domain.CatalogEntry, enrich bool) (bool, error) {
	var queue bool
	run := func() error {
		return d.s.store.InTx(ctx, func(q *store.Queries) error {
			if err := upsertCatalogEntry(ctx, q, e); err != nil {
				return err
			}
			if !enrich {
				return nil
			}
			var err error
			queue, err = q.EnsureTrackFeatures(ctx, e.Track.ID, e.Track.ISRC)
			return err
		})
	}
	err := run()
	// A RecordPlayback upserting the same rows at the same moment can
	// deadlock with this transaction; Postgres aborts one, so retry once.
	if isDeadlock(err) {
		err = run()
	}
	return queue, err
}

func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40P01" || pgErr.Code == "40001")
}

func (d serviceDeps) CurrentTrack(ctx context.Context) (string, bool, error) {
	return d.s.store.CurrentTrack(ctx)
}

func (d serviceDeps) SetUpNext(ctx context.Context, body []byte) error {
	return d.s.rdb.Set(ctx, redisx.KeyUpNext, body, upNextTTL).Err()
}

func (d serviceDeps) ClearUpNext(ctx context.Context) error {
	return d.s.rdb.Del(ctx, redisx.KeyUpNext).Err()
}

func (d serviceDeps) MarkCatalogSeen(ctx context.Context, trackID string) {
	_ = d.s.rdb.Set(ctx, redisx.KeyCatalogSeen(trackID), 1, catalogSeenTTL).Err()
}

func (d serviceDeps) QueueEnrichment(ctx context.Context, trackID, isrc string) {
	d.s.queueEnrichment(ctx, trackID, isrc)
}
