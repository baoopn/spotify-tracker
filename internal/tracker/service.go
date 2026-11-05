// Package tracker implements the playback ingestion and enrichment use cases.
// It is the only component that writes to the database.
package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/observability"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/store"
)

const (
	catalogSeenTTL   = 6 * time.Hour
	playbackStateTTL = 30 * time.Minute
	nowPlayingTTL    = 10 * time.Minute
	enrichStreamMax  = 10_000
)

// PlaylistFetcher looks up playlist metadata (implemented by *spotify.Manager).
type PlaylistFetcher interface {
	Playlist(ctx context.Context, id string) (domain.Playlist, error)
}

// Service holds the dependencies of the tracker use cases.
type Service struct {
	store       *store.Store
	rdb         *redis.Client
	log         *slog.Logger
	threshold   time.Duration
	maxAttempts int
	playlists   PlaylistFetcher
}

// SetPlaylistFetcher enables playlist metadata lookups for played contexts.
func (s *Service) SetPlaylistFetcher(f PlaylistFetcher) { s.playlists = f }

// NewService creates a Service.
func NewService(st *store.Store, rdb *redis.Client, log *slog.Logger, replayThreshold time.Duration, maxAttempts int) *Service {
	return &Service{store: st, rdb: rdb, log: log, threshold: replayThreshold, maxAttempts: maxAttempts}
}

// RecordResult reports the side effects of RecordPlayback.
type RecordResult struct {
	HistoryCreated    bool
	ReplayIncremented bool
	EnrichmentQueued  bool
}

// RecordPlayback persists a playback snapshot: catalog upserts, the current
// playback row and history/replay changes happen in one transaction; cache,
// queue and notification side effects follow after commit.
func (s *Service) RecordPlayback(ctx context.Context, pb domain.Playback) (RecordResult, error) {
	if pb.Track.ID == "" {
		return RecordResult{}, errors.New("track id is required")
	}
	pb = normalizePlayback(pb)

	prev, err := s.loadState(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "load playback state failed; assuming none", logging.Err(err))
	}
	decision := Decide(prev, pb.Track.ID, pb.ProgressMS, s.threshold)
	catalogKnown := s.catalogKnown(ctx, pb.Track.ID)

	var res RecordResult
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if !catalogKnown {
			queued, err := upsertCatalog(ctx, q, pb)
			if err != nil {
				return err
			}
			res.EnrichmentQueued = queued
		}
		if err := q.SetCurrentPlayback(ctx, pb.Track.ID, pb.IsPlaying, pb.ProgressMS, pb.FetchedAt); err != nil {
			return err
		}
		return applyHistory(ctx, q, pb, decision, &res)
	})
	if err != nil {
		return res, fmt.Errorf("record playback: %w", err)
	}

	s.afterRecord(ctx, pb, res, catalogKnown)
	return res, nil
}

// normalizePlayback fills defaults for optional snapshot fields.
func normalizePlayback(pb domain.Playback) domain.Playback {
	if pb.FetchedAt.IsZero() {
		pb.FetchedAt = time.Now().UTC()
	}
	if pb.PlayedAt.IsZero() {
		pb.PlayedAt = pb.FetchedAt
	}
	pb.Track.AlbumID = pb.Album.ID
	return pb
}

// catalogKnown reports whether the track's catalog rows were upserted
// recently (Redis marker); errors count as unknown so rows get refreshed.
func (s *Service) catalogKnown(ctx context.Context, trackID string) bool {
	n, err := s.rdb.Exists(ctx, redisx.KeyCatalogSeen(trackID)).Result()
	return err == nil && n > 0
}

// upsertCatalog writes artists, album and track, and ensures an enrichment
// row. It returns true when the track should be queued for enrichment.
func upsertCatalog(ctx context.Context, q *store.Queries, pb domain.Playback) (bool, error) {
	ids := make([]string, 0, len(pb.Artists))
	for _, a := range pb.Artists {
		if err := q.UpsertArtist(ctx, a); err != nil {
			return false, err
		}
		ids = append(ids, a.ID)
	}
	if pb.Album.ID != "" {
		if err := q.UpsertAlbum(ctx, pb.Album); err != nil {
			return false, err
		}
	}
	if err := q.UpsertTrack(ctx, pb.Track, ids); err != nil {
		return false, err
	}
	return q.EnsureTrackFeatures(ctx, pb.Track.ID, pb.Track.ISRC)
}

// applyHistory appends a history row or counts a replay, per decision.
func applyHistory(ctx context.Context, q *store.Queries, pb domain.Playback, d Decision, res *RecordResult) error {
	if d.Replay {
		incremented, err := incrementReplayIfLatest(ctx, q, pb)
		if err != nil {
			return err
		}
		res.ReplayIncremented = incremented
	}
	if d.NewHistory {
		created, err := q.InsertHistory(ctx, pb.Track.ID, pb.PlayedAt, pb.FetchedAt, pb.Track.DurationMS, 1, pb.Context)
		if err != nil {
			return err
		}
		res.HistoryCreated = created
	}
	return nil
}

// incrementReplayIfLatest bumps the replay count when the newest history
// row is the same track.
func incrementReplayIfLatest(ctx context.Context, q *store.Queries, pb domain.Playback) (bool, error) {
	id, trackID, err := q.LatestHistory(ctx)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && trackID != pb.Track.ID) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := q.IncrementReplay(ctx, id, pb.FetchedAt); err != nil {
		return false, err
	}
	return true, nil
}

// afterRecord runs best-effort post-commit side effects. Failures are logged,
// never returned: the database is the source of truth.
func (s *Service) afterRecord(ctx context.Context, pb domain.Playback, res RecordResult, catalogKnown bool) {
	if !catalogKnown {
		_ = s.rdb.Set(ctx, redisx.KeyCatalogSeen(pb.Track.ID), 1, catalogSeenTTL).Err()
	}
	if err := s.saveState(ctx, State{TrackID: pb.Track.ID, ProgressMS: pb.ProgressMS}); err != nil {
		s.log.WarnContext(ctx, "save playback state failed", logging.Err(err))
	}
	if res.EnrichmentQueued {
		s.queueEnrichment(ctx, pb.Track.ID, pb.Track.ISRC)
	}
	if res.HistoryCreated && pb.Context != nil && pb.Context.Type == "playlist" {
		s.refreshPlaylist(pb.Context)
	}
	s.publishNowPlaying(ctx)

	if res.HistoryCreated {
		observability.HistoryEntries.Inc()
		s.log.InfoContext(ctx, "new listening history entry",
			slog.String("track_id", pb.Track.ID), slog.String("track", pb.Track.Name))
	}
	if res.ReplayIncremented {
		observability.Replays.Inc()
		s.log.InfoContext(ctx, "replay counted", slog.String("track_id", pb.Track.ID))
	}
}

// ClearPlayback records that nothing is playing. Like the original poller it
// forgets the last track, so resuming later starts a new history entry.
func (s *Service) ClearPlayback(ctx context.Context, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	changed, err := s.store.MarkNotPlaying(ctx, at)
	if err != nil {
		return err
	}
	if err := s.rdb.Del(ctx, redisx.KeyPlaybackState).Err(); err != nil {
		s.log.WarnContext(ctx, "clear playback state failed", logging.Err(err))
	}
	if changed {
		s.publishNowPlaying(ctx)
	}
	return nil
}

// ListPending returns tracks due for enrichment.
func (s *Service) ListPending(ctx context.Context, limit int) ([]domain.PendingTrack, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	return s.store.ListPending(ctx, limit)
}

// SubmitFeatures stores an enrichment result.
func (s *Service) SubmitFeatures(ctx context.Context, trackID string, found bool, f domain.AudioFeatures, genres []domain.Genre, source string) error {
	if err := s.store.SaveFeatures(ctx, trackID, found, f, genres, source); err != nil {
		return err
	}
	status := domain.EnrichmentEnriched
	if !found {
		status = domain.EnrichmentUnavailable
	}
	observability.EnrichmentResults.WithLabelValues(status).Inc()
	return nil
}

// ReportFailure schedules a retry for a failed enrichment.
func (s *Service) ReportFailure(ctx context.Context, trackID, msg string) (bool, error) {
	exhausted, err := s.store.RecordFailure(ctx, trackID, msg, s.maxAttempts)
	if err != nil {
		return false, err
	}
	observability.EnrichmentResults.WithLabelValues(domain.EnrichmentFailed).Inc()
	if exhausted {
		s.log.WarnContext(ctx, "enrichment attempts exhausted", slog.String("track_id", trackID))
	}
	return exhausted, nil
}

func (s *Service) loadState(ctx context.Context) (State, error) {
	m, err := s.rdb.HGetAll(ctx, redisx.KeyPlaybackState).Result()
	if err != nil {
		return State{}, err
	}
	st := State{TrackID: m["track_id"]}
	if v, ok := m["progress_ms"]; ok && v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			p := int32(n)
			st.ProgressMS = &p
		}
	}
	return st, nil
}

func (s *Service) saveState(ctx context.Context, st State) error {
	progress := ""
	if st.ProgressMS != nil {
		progress = strconv.FormatInt(int64(*st.ProgressMS), 10)
	}
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, redisx.KeyPlaybackState, "track_id", st.TrackID, "progress_ms", progress)
	pipe.Expire(ctx, redisx.KeyPlaybackState, playbackStateTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// QueueEnrichment asks the enricher to process a track now.
func (s *Service) QueueEnrichment(ctx context.Context, trackID, isrc string) {
	s.queueEnrichment(ctx, trackID, isrc)
}

func (s *Service) queueEnrichment(ctx context.Context, trackID, isrc string) {
	err := s.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: redisx.StreamEnrich,
		MaxLen: enrichStreamMax,
		Approx: true,
		Values: map[string]any{"track_id": trackID, "isrc": isrc},
	}).Err()
	if err != nil {
		// The enricher's periodic sweep will still pick the track up.
		s.log.WarnContext(ctx, "queue enrichment failed", slog.String("track_id", trackID), logging.Err(err))
	}
}

// publishNowPlaying refreshes the now-playing cache and notifies live
// subscribers (SSE clients).
func (s *Service) publishNowPlaying(ctx context.Context) {
	np, err := s.store.NowPlaying(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "load now playing failed", logging.Err(err))
		return
	}
	body, err := json.Marshal(np)
	if err != nil {
		return
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, redisx.KeyNowPlaying, body, nowPlayingTTL)
	pipe.Publish(ctx, redisx.ChannelPlayback, body)
	if _, err := pipe.Exec(ctx); err != nil {
		s.log.WarnContext(ctx, "publish now playing failed", logging.Err(err))
	}
}

// playlistTTL is how long fetched playlist metadata is considered fresh.
const playlistTTL = 24 * time.Hour

// refreshPlaylist fetches playlist metadata in the background when it is
// missing or stale. A Redis lock prevents concurrent fetches of one playlist.
func (s *Service) refreshPlaylist(pc *domain.PlaybackContext) {
	id := pc.ID()
	if s.playlists == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if at, err := s.store.PlaylistFetchedAt(ctx, id); err == nil && time.Since(at) < playlistTTL {
			return
		}
		ok, err := s.rdb.SetNX(ctx, redisx.KeyPlaylistLock(id), 1, 10*time.Minute).Result()
		if err != nil || !ok {
			return
		}
		pl, err := s.playlists.Playlist(ctx, id)
		if err != nil {
			s.log.WarnContext(ctx, "fetch playlist failed", slog.String("playlist_id", id), logging.Err(err))
			return
		}
		if pl.URI == "" {
			pl.URI = pc.URI
		}
		if pl.ExternalURL == "" {
			pl.ExternalURL = pc.ExternalURL
		}
		if err := s.store.UpsertPlaylist(ctx, pl); err != nil {
			s.log.WarnContext(ctx, "store playlist failed", slog.String("playlist_id", id), logging.Err(err))
			return
		}
		s.log.InfoContext(ctx, "playlist metadata stored", slog.String("playlist_id", id),
			slog.String("name", pl.Name), slog.Bool("available", pl.Available))
	}()
}

// RefreshPlaylist forces a metadata refresh for a playlist (admin action).
func (s *Service) RefreshPlaylist(ctx context.Context, id string) (domain.Playlist, error) {
	if s.playlists == nil {
		return domain.Playlist{}, errors.New("playlist lookups are not configured")
	}
	pl, err := s.playlists.Playlist(ctx, id)
	if err != nil {
		return pl, err
	}
	if pl.URI == "" {
		pl.URI = "spotify:playlist:" + id
	}
	if pl.ExternalURL == "" {
		pl.ExternalURL = "https://open.spotify.com/playlist/" + id
	}
	return pl, s.store.UpsertPlaylist(ctx, pl)
}
