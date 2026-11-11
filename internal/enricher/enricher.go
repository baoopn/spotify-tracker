// Package enricher fetches audio features and genres for new tracks.
//
// Work arrives two ways: a Redis stream that the API appends to when it sees
// a new track (low latency), and a periodic gRPC sweep of pending tracks
// (catches anything missed, plus scheduled retries).
package enricher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/grpcapi"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/soundcharts"
)

const source = "soundcharts"

// Provider looks up a song by ISRC (implemented by *soundcharts.Client).
type Provider interface {
	SongByISRC(ctx context.Context, isrc string) (*soundcharts.Song, error)
}

// Heartbeat publishes worker status (implemented by *heartbeat.Reporter).
type Heartbeat interface {
	OK(ctx context.Context, detail string)
	Fail(ctx context.Context, err error)
}

// Enricher runs the enrichment loops.
type Enricher struct {
	provider    Provider
	api         trackerv1.EnrichmentServiceClient
	rdb         *redis.Client
	interval    time.Duration
	batchSize   int
	concurrency int
	consumer    string
	hb          Heartbeat
	log         *slog.Logger

	lastSweep atomic.Int64
}

// New creates an Enricher.
func New(p Provider, api trackerv1.EnrichmentServiceClient, rdb *redis.Client, interval time.Duration, batchSize, concurrency int, hb Heartbeat, log *slog.Logger) *Enricher {
	host, _ := os.Hostname()
	if concurrency < 1 {
		concurrency = 1
	}
	e := &Enricher{provider: p, api: api, rdb: rdb, interval: interval, batchSize: batchSize,
		concurrency: concurrency, consumer: "enricher-" + host, hb: hb, log: log}
	e.lastSweep.Store(time.Now().UnixNano())
	return e
}

// Ready reports unhealthy if no sweep succeeded within 5 intervals (min 10m).
func (e *Enricher) Ready(context.Context) error {
	window := max(5*e.interval, 10*time.Minute)
	last := time.Unix(0, e.lastSweep.Load())
	if time.Since(last) > window {
		return fmt.Errorf("no successful sweep since %s", last.UTC().Format(time.RFC3339))
	}
	return nil
}

// RunSweeps periodically enriches pending tracks until ctx is cancelled.
func (e *Enricher) RunSweeps(ctx context.Context) error {
	e.log.Info("enrichment sweep started", slog.Duration("interval", e.interval))
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		n, err := e.sweep(ctx)
		switch {
		case err != nil && !errors.Is(err, context.Canceled):
			e.log.Error("enrichment sweep failed", logging.Err(err))
			e.hb.Fail(ctx, err)
		case err == nil:
			e.lastSweep.Store(time.Now().UnixNano())
			e.hb.OK(ctx, fmt.Sprintf("last sweep processed %d track(s)", n))
		}
		t.Reset(e.interval)
	}
}

func (e *Enricher) sweep(ctx context.Context) (int, error) {
	processed := 0
	for {
		resp, err := e.api.ListPendingTracks(ctx, &trackerv1.ListPendingTracksRequest{Limit: int32(e.batchSize)})
		if err != nil {
			return processed, fmt.Errorf("list pending: %w", err)
		}
		if len(resp.GetTracks()) == 0 {
			return processed, nil
		}
		processed += len(resp.GetTracks())
		e.log.Info("enriching pending tracks", slog.Int("count", len(resp.GetTracks())))
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(e.concurrency)
		for _, t := range resp.GetTracks() {
			if gctx.Err() != nil {
				break
			}
			g.Go(func() error {
				e.enrich(gctx, t.GetTrackId(), t.GetIsrc())
				return nil
			})
		}
		_ = g.Wait()
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		// Tracks that failed are rescheduled into the future, so the next page
		// only contains different tracks; stop when a page is not full.
		if len(resp.GetTracks()) < e.batchSize {
			return processed, nil
		}
	}
}

// RunStream consumes the enrichment stream until ctx is cancelled.
func (e *Enricher) RunStream(ctx context.Context) error {
	err := e.rdb.XGroupCreateMkStream(ctx, redisx.StreamEnrich, redisx.GroupEnrich, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return fmt.Errorf("create consumer group: %w", err)
	}
	e.log.Info("enrichment stream consumer started", slog.String("consumer", e.consumer))
	for {
		if ctx.Err() != nil {
			return nil
		}
		streams, err := e.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: redisx.GroupEnrich, Consumer: e.consumer,
			Streams: []string{redisx.StreamEnrich, ">"},
			Count:   10, Block: 30 * time.Second,
		}).Result()
		switch {
		case errors.Is(err, redis.Nil):
			continue
		case ctx.Err() != nil:
			return nil
		case err != nil:
			e.log.Warn("read enrichment stream failed", logging.Err(err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(e.concurrency)
		for _, s := range streams {
			for _, msg := range s.Messages {
				g.Go(func() error {
					trackID, _ := msg.Values["track_id"].(string)
					isrc, _ := msg.Values["isrc"].(string)
					if trackID != "" {
						e.enrich(gctx, trackID, isrc)
					}
					// Ack regardless: failures are tracked in the database and
					// retried by the sweep with backoff.
					_ = e.rdb.XAck(ctx, redisx.StreamEnrich, redisx.GroupEnrich, msg.ID).Err()
					return nil
				})
			}
		}
		_ = g.Wait()
	}
}

func (e *Enricher) enrich(ctx context.Context, trackID, isrc string) {
	log := e.log.With(slog.String("track_id", trackID), slog.String("isrc", isrc))
	if isrc == "" {
		// Without an ISRC there is nothing to look up.
		if err := e.submit(ctx, trackID, nil); err != nil {
			log.Error("mark unavailable failed", logging.Err(err))
		}
		log.Warn("track has no ISRC; marked unavailable")
		return
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	song, err := e.provider.SongByISRC(cctx, isrc)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		var re *soundcharts.RetryableError
		if errors.As(err, &re) && re.RetryAfter > 0 {
			log.Warn("soundcharts asked to retry later", slog.Duration("retry_after", re.RetryAfter))
		}
		resp, rerr := e.api.ReportEnrichmentFailure(ctx, failureRequest(trackID, err))
		if rerr != nil {
			log.Error("report failure failed", logging.Err(rerr))
		}
		log.Warn("enrichment failed; will retry", logging.Err(err), slog.Bool("exhausted", resp.GetExhausted()))
		return
	}
	if err := e.submit(ctx, trackID, song); err != nil {
		log.Error("submit features failed", logging.Err(err))
		return
	}
	if song == nil || (!song.HasAudio && len(song.Genres) == 0) {
		log.Info("no data at provider; marked unavailable")
	} else {
		log.Info("track enriched", slog.Bool("audio", song.HasAudio), slog.Int("genres", len(song.Genres)))
	}
}

func (e *Enricher) submit(ctx context.Context, trackID string, song *soundcharts.Song) error {
	req := &trackerv1.SubmitTrackFeaturesRequest{TrackId: trackID, Source: source}
	if song != nil && (song.HasAudio || len(song.Genres) > 0) {
		req.Found = true
		req.Features = grpcapi.FeaturesToProto(song.Features)
		for _, g := range song.Genres {
			req.Genres = append(req.Genres, &trackerv1.Genre{Root: g.Root, Sub: g.Sub})
		}
	}
	_, err := e.api.SubmitTrackFeatures(ctx, req)
	return err
}

func isBusyGroup(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "BUSYGROUP")
}

// failureRequest reports err for trackID. A soundcharts.RetryableError (429,
// 5xx, network) is marked retryable, with its Retry-After, so the API pushes
// the retry back without using up an attempt.
func failureRequest(trackID string, err error) *trackerv1.ReportEnrichmentFailureRequest {
	req := &trackerv1.ReportEnrichmentFailureRequest{TrackId: trackID, Error: err.Error()}
	var re *soundcharts.RetryableError
	if errors.As(err, &re) {
		req.Retryable = true
		req.RetryAfterSeconds = int32(min(re.RetryAfter/time.Second, math.MaxInt32))
	}
	return req
}
