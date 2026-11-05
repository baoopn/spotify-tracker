// Package poller polls Spotify for the currently playing track and forwards
// snapshots to the tracker API over gRPC.
package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/grpcapi"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/observability"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// Source fetches the current playback (implemented by *spotify.Player).
type Source interface {
	CurrentlyPlaying(ctx context.Context) (*domain.Playback, error)
}

// Heartbeat publishes the poller status (implemented by *heartbeat.Reporter).
type Heartbeat interface {
	OK(ctx context.Context, detail string)
	Fail(ctx context.Context, err error)
}

// Poller runs the poll loop.
type Poller struct {
	src      Source
	ingest   trackerv1.IngestServiceClient
	interval time.Duration
	hb       Heartbeat
	log      *slog.Logger

	lastSuccess atomic.Int64 // unix nanos
	wasPlaying  bool
	lastTrackID string
}

// New creates a Poller.
func New(src Source, ingest trackerv1.IngestServiceClient, interval time.Duration, hb Heartbeat, log *slog.Logger) *Poller {
	p := &Poller{src: src, ingest: ingest, interval: interval, hb: hb, log: log}
	p.lastSuccess.Store(time.Now().UnixNano())
	return p
}

// Ready reports unhealthy if no cycle succeeded within 20 intervals (min 2m).
func (p *Poller) Ready(context.Context) error {
	window := max(20*p.interval, 2*time.Minute)
	last := time.Unix(0, p.lastSuccess.Load())
	if time.Since(last) > window {
		return fmt.Errorf("no successful poll since %s", last.UTC().Format(time.RFC3339))
	}
	return nil
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	p.log.Info("poller started", slog.Duration("interval", p.interval))
	wait := time.Duration(0)
	consecutiveErrors := 0
	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			p.log.Info("poller stopped")
			return nil
		case <-timer.C:
		}

		detail, err := p.cycle(ctx)
		wait = p.interval
		var rl *spotify.RateLimitError
		switch {
		case err == nil:
			consecutiveErrors = 0
			p.lastSuccess.Store(time.Now().UnixNano())
			p.hb.OK(ctx, detail)
			continue
		case errors.Is(err, context.Canceled):
			return nil
		case errors.As(err, &rl):
			observability.PollCycles.WithLabelValues("rate_limited").Inc()
			wait = max(rl.RetryAfter, p.interval)
			p.log.Warn("spotify rate limited; backing off",
				slog.Duration("retry_after", wait), slog.String("reason", rl.Reason))
		case errors.Is(err, grpcapi.ErrSpotifyAuth):
			observability.PollCycles.WithLabelValues("error").Inc()
			// Needs operator action (re-authorize in the dashboard); poll
			// slowly until the token works again.
			wait = 30 * time.Second
			p.log.Error("spotify authorization required; re-authorize in the admin dashboard", logging.Err(err))
		default:
			observability.PollCycles.WithLabelValues("error").Inc()
			consecutiveErrors++
			// Exponential backoff on repeated failures, capped at 1 minute.
			wait = min(p.interval*time.Duration(1<<min(consecutiveErrors-1, 5)), time.Minute)
			p.log.Error("poll cycle failed", logging.Err(err), slog.Int("consecutive", consecutiveErrors))
		}
		p.hb.Fail(ctx, err)
	}
}

func (p *Poller) cycle(ctx context.Context) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	pb, err := p.src.CurrentlyPlaying(cctx)
	if err != nil {
		return "", err
	}
	if pb == nil {
		observability.PollCycles.WithLabelValues("idle").Inc()
		if p.wasPlaying || p.lastTrackID != "" {
			if _, err := p.ingest.ClearPlayback(cctx, &trackerv1.ClearPlaybackRequest{
				FetchedAt: timestamppb.Now(),
			}); err != nil {
				return "", fmt.Errorf("clear playback: %w", err)
			}
			p.log.Info("nothing playing")
		}
		p.wasPlaying, p.lastTrackID = false, ""
		return "idle", nil
	}

	observability.PollCycles.WithLabelValues("playing").Inc()
	res, err := p.ingest.RecordPlayback(cctx, &trackerv1.RecordPlaybackRequest{Playback: grpcapi.PlaybackToProto(*pb)})
	if err != nil {
		return "", fmt.Errorf("record playback: %w", err)
	}
	state := "paused"
	if pb.IsPlaying {
		state = "playing"
	}
	if pb.Track.ID != p.lastTrackID || pb.IsPlaying != p.wasPlaying {
		p.log.Info("playback synced",
			slog.String("track_id", pb.Track.ID), slog.String("track", pb.Track.Name),
			slog.String("state", state), slog.Bool("history_created", res.GetHistoryCreated()))
	}
	p.wasPlaying, p.lastTrackID = pb.IsPlaying, pb.Track.ID
	return state + ": " + pb.Track.Name, nil
}
