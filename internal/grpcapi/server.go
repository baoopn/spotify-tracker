// Package grpcapi exposes the tracker use cases over internal gRPC.
package grpcapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/observability"
	"github.com/baoopn/spotify-tracker/internal/spotify"
	"github.com/baoopn/spotify-tracker/internal/tracker"
)

// NewServer builds a gRPC server with auth, logging, recovery and health.
func NewServer(svc *tracker.Service, mgr *spotify.Manager, token string, log *slog.Logger, enableReflection bool) (*grpc.Server, *health.Server) {
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(recoverInterceptor(log), loggingInterceptor(log), authInterceptor(token)),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: true}),
		grpc.MaxRecvMsgSize(4<<20),
	)
	trackerv1.RegisterIngestServiceServer(srv, &ingestServer{svc: svc})
	trackerv1.RegisterEnrichmentServiceServer(srv, &enrichmentServer{svc: svc})
	trackerv1.RegisterSpotifyServiceServer(srv, &spotifyServer{mgr: mgr})

	hs := health.NewServer()
	healthpb.RegisterHealthServer(srv, hs)
	if enableReflection {
		reflection.Register(srv)
	}
	return srv, hs
}

// ─── Interceptors ─────────────────────────────────────────────────────────────

func authInterceptor(token string) grpc.UnaryServerInterceptor {
	want := []byte("Bearer " + token)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if strings.HasPrefix(info.FullMethod, "/grpc.health.v1.Health/") ||
			strings.HasPrefix(info.FullMethod, "/grpc.reflection.") {
			return handler(ctx, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		vals := md.Get("authorization")
		if len(vals) != 1 || subtle.ConstantTimeCompare([]byte(vals[0]), want) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid or missing token")
		}
		return handler(ctx, req)
	}
}

func loggingInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := status.Code(err)
		observability.GRPCRequests.WithLabelValues(info.FullMethod, code.String()).Inc()
		attrs := []any{
			slog.String("method", info.FullMethod), slog.String("code", code.String()),
			slog.Duration("duration", time.Since(start)),
		}
		level := slog.LevelDebug
		if code != codes.OK {
			level = slog.LevelWarn
			attrs = append(attrs, logging.Err(err))
		}
		log.Log(ctx, level, "grpc request", attrs...)
		return resp, err
	}
}

func recoverInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if p := recover(); p != nil {
				log.ErrorContext(ctx, "grpc panic", slog.String("method", info.FullMethod), slog.Any("panic", p))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

func toStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// ─── IngestService ────────────────────────────────────────────────────────────

type ingestServer struct {
	trackerv1.UnimplementedIngestServiceServer
	svc *tracker.Service
}

func (s *ingestServer) RecordPlayback(ctx context.Context, req *trackerv1.RecordPlaybackRequest) (*trackerv1.RecordPlaybackResponse, error) {
	pb := req.GetPlayback()
	if pb.GetTrack().GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "playback.track.id is required")
	}
	if pb.GetTrack().GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "playback.track.name is required")
	}
	res, err := s.svc.RecordPlayback(ctx, PlaybackFromProto(pb))
	if err != nil {
		return nil, toStatus(err)
	}
	return &trackerv1.RecordPlaybackResponse{
		HistoryCreated:    res.HistoryCreated,
		ReplayIncremented: res.ReplayIncremented,
		EnrichmentQueued:  res.EnrichmentQueued,
	}, nil
}

func (s *ingestServer) ClearPlayback(ctx context.Context, req *trackerv1.ClearPlaybackRequest) (*trackerv1.ClearPlaybackResponse, error) {
	var at time.Time
	if req.GetFetchedAt() != nil {
		at = req.GetFetchedAt().AsTime()
	}
	if err := s.svc.ClearPlayback(ctx, at); err != nil {
		return nil, toStatus(err)
	}
	return &trackerv1.ClearPlaybackResponse{}, nil
}

// ─── EnrichmentService ────────────────────────────────────────────────────────

type enrichmentServer struct {
	trackerv1.UnimplementedEnrichmentServiceServer
	svc *tracker.Service
}

func (s *enrichmentServer) ListPendingTracks(ctx context.Context, req *trackerv1.ListPendingTracksRequest) (*trackerv1.ListPendingTracksResponse, error) {
	items, err := s.svc.ListPending(ctx, int(req.GetLimit()))
	if err != nil {
		return nil, toStatus(err)
	}
	out := &trackerv1.ListPendingTracksResponse{Tracks: make([]*trackerv1.PendingTrack, 0, len(items))}
	for _, p := range items {
		out.Tracks = append(out.Tracks, &trackerv1.PendingTrack{TrackId: p.TrackID, Isrc: p.ISRC, Attempts: p.Attempts})
	}
	return out, nil
}

func (s *enrichmentServer) SubmitTrackFeatures(ctx context.Context, req *trackerv1.SubmitTrackFeaturesRequest) (*trackerv1.SubmitTrackFeaturesResponse, error) {
	if req.GetTrackId() == "" {
		return nil, status.Error(codes.InvalidArgument, "track_id is required")
	}
	genres := make([]domain.Genre, 0, len(req.GetGenres()))
	for _, g := range req.GetGenres() {
		sub := g.GetSub()
		if sub == nil {
			sub = []string{}
		}
		genres = append(genres, domain.Genre{Root: g.GetRoot(), Sub: sub})
	}
	if err := s.svc.SubmitFeatures(ctx, req.GetTrackId(), req.GetFound(),
		FeaturesFromProto(req.GetFeatures()), genres, req.GetSource()); err != nil {
		return nil, toStatus(err)
	}
	return &trackerv1.SubmitTrackFeaturesResponse{}, nil
}

func (s *enrichmentServer) ReportEnrichmentFailure(ctx context.Context, req *trackerv1.ReportEnrichmentFailureRequest) (*trackerv1.ReportEnrichmentFailureResponse, error) {
	if req.GetTrackId() == "" {
		return nil, status.Error(codes.InvalidArgument, "track_id is required")
	}
	retryAfter := time.Duration(max(req.GetRetryAfterSeconds(), 0)) * time.Second
	exhausted, err := s.svc.ReportFailure(ctx, req.GetTrackId(), req.GetError(), req.GetRetryable(), retryAfter)
	if err != nil {
		return nil, toStatus(err)
	}
	return &trackerv1.ReportEnrichmentFailureResponse{Exhausted: exhausted}, nil
}
