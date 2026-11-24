package tracker

import (
	"context"
	"strconv"
	"time"

	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
)

// Redis hash fields of the listen in progress (redisx.KeyPlaybackState).
const (
	fieldTrackID    = "track_id"
	fieldProgressMS = "progress_ms"
	fieldStartedAt  = "started_at"  // unix ms
	fieldListenedMS = "listened_ms" // ms heard
	fieldCounted    = "counted"     // "1" once written as a play
	fieldReplay     = "replay"      // "1" for a replay listen
	fieldLastSeen   = "last_seen"   // unix ms
)

func (s *Service) loadListen(ctx context.Context, now time.Time) (Listen, error) {
	m, err := s.rdb.HGetAll(ctx, redisx.KeyPlaybackState).Result()
	if err != nil {
		return Listen{}, err
	}
	return decodeListen(m, now), nil
}

// decodeListen reads the stored listen. State written before the play-count
// rules ({track_id, progress_ms} only) belonged to a track whose history row
// was already written on its first snapshot, so it is read as a counted
// listen seen now: deploying never counts the current song twice.
func decodeListen(m map[string]string, now time.Time) Listen {
	l := Listen{TrackID: m[fieldTrackID]}
	if l.TrackID == "" {
		return Listen{}
	}
	if v, err := strconv.ParseInt(m[fieldProgressMS], 10, 32); err == nil {
		p := int32(v)
		l.ProgressMS = &p
	}
	startedMS, err := strconv.ParseInt(m[fieldStartedAt], 10, 64)
	if err != nil {
		l.Counted = true
		l.StartedAt = now
		l.LastSeen = now
		return l
	}
	l.StartedAt = time.UnixMilli(startedMS).UTC()
	l.ListenedMS, _ = strconv.ParseInt(m[fieldListenedMS], 10, 64)
	l.Counted = m[fieldCounted] == "1"
	l.Replay = m[fieldReplay] == "1"
	if seen, err := strconv.ParseInt(m[fieldLastSeen], 10, 64); err == nil {
		l.LastSeen = time.UnixMilli(seen).UTC()
	} else {
		l.LastSeen = l.StartedAt
	}
	return l
}

// encodeListen is the inverse of decodeListen.
func encodeListen(l Listen) map[string]any {
	progress := ""
	if l.ProgressMS != nil {
		progress = strconv.FormatInt(int64(*l.ProgressMS), 10)
	}
	return map[string]any{
		fieldTrackID:    l.TrackID,
		fieldProgressMS: progress,
		fieldStartedAt:  strconv.FormatInt(l.StartedAt.UnixMilli(), 10),
		fieldListenedMS: strconv.FormatInt(l.ListenedMS, 10),
		fieldCounted:    boolField(l.Counted),
		fieldReplay:     boolField(l.Replay),
		fieldLastSeen:   strconv.FormatInt(l.LastSeen.UnixMilli(), 10),
	}
}

func boolField(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// saveListen stores the listen for the resume window, after which a resume
// of the same track starts a new listen anyway.
func (s *Service) saveListen(ctx context.Context, l Listen) error {
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, redisx.KeyPlaybackState)
	pipe.HSet(ctx, redisx.KeyPlaybackState, encodeListen(l))
	pipe.Expire(ctx, redisx.KeyPlaybackState, s.rules.ResumeWindow)
	_, err := pipe.Exec(ctx)
	return err
}
