package tracker

import (
	"context"
	"strconv"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// playbackTx is the part of *store.Queries the play-count path uses inside
// the RecordPlayback transaction (an interface so tests can use a fake).
type playbackTx interface {
	LockListenState(ctx context.Context) (store.ListenState, error)
	SaveListenState(ctx context.Context, st store.ListenState) error
	SetCurrentPlayback(ctx context.Context, trackID string, isPlaying bool, progressMS *int32, fetchedAt time.Time) error
	InsertHistory(ctx context.Context, trackID string, playedAt, updatedAt time.Time, durationMS int32, replayCount int32, pc *domain.PlaybackContext) (bool, error)
	IncrementReplayAt(ctx context.Context, trackID string, playedAt, at time.Time) (bool, error)
	LatestHistory(ctx context.Context) (id, trackID string, err error)
	IncrementReplay(ctx context.Context, id string, at time.Time) error
}

// legacyLoader reads the listen state the Redis-based rules left behind.
type legacyLoader func(ctx context.Context, now time.Time) Listen

// recordListen locks the listen in progress, applies the snapshot and
// writes current_playback, the history change and the new listen, all in
// the caller's transaction. On any error nothing is kept.
func recordListen(ctx context.Context, q playbackTx, legacy legacyLoader, pb domain.Playback, rules PlayRules, res *RecordResult) (Listen, bool, error) {
	row, err := q.LockListenState(ctx)
	if err != nil {
		return Listen{}, false, err
	}
	prev := listenFromState(row)
	usedLegacy := false
	if prev.TrackID == "" && legacy != nil {
		prev = legacy(ctx, pb.FetchedAt)
		usedLegacy = prev.TrackID != ""
	}
	listen, d := Step(prev, Snapshot{
		TrackID: pb.Track.ID, ProgressMS: pb.ProgressMS, DurationMS: pb.Track.DurationMS,
		IsPlaying: pb.IsPlaying, PlayedAt: pb.PlayedAt, At: pb.FetchedAt,
	}, rules)
	if err := q.SetCurrentPlayback(ctx, pb.Track.ID, pb.IsPlaying, pb.ProgressMS, pb.FetchedAt); err != nil {
		return Listen{}, false, err
	}
	if err := applyHistory(ctx, q, pb, d, res); err != nil {
		return Listen{}, false, err
	}
	if err := q.SaveListenState(ctx, stateFromListen(listen)); err != nil {
		return Listen{}, false, err
	}
	return listen, usedLegacy, nil
}

func listenFromState(st store.ListenState) Listen {
	return Listen{
		TrackID: st.TrackID, StartedAt: st.StartedAt, ProgressMS: st.ProgressMS, ListenedMS: st.ListenedMS,
		Counted: st.Counted, Replay: st.Replay, RowAt: st.RowPlayedAt, AppliedAt: st.AppliedAt, LastSeen: st.LastSeen,
	}
}

func stateFromListen(l Listen) store.ListenState {
	return store.ListenState{
		TrackID: l.TrackID, StartedAt: l.StartedAt, ProgressMS: l.ProgressMS, ListenedMS: l.ListenedMS,
		Counted: l.Counted, Replay: l.Replay, RowPlayedAt: l.RowAt, AppliedAt: l.AppliedAt, LastSeen: l.LastSeen,
	}
}

// loadLegacyListen reads the Redis hash {track_id, progress_ms} written by
// the rules before listen_state existed. Its track was written to history on
// its first snapshot, so it is read as a counted listen seen now: the song
// playing during the upgrade is not counted twice. A Redis error reads as
// no state.
func (s *Service) loadLegacyListen(ctx context.Context, now time.Time) Listen {
	m, err := s.rdb.HGetAll(ctx, redisx.KeyPlaybackState).Result()
	if err != nil {
		return Listen{}
	}
	return decodeLegacyListen(m, now)
}

func decodeLegacyListen(m map[string]string, now time.Time) Listen {
	l := Listen{TrackID: m["track_id"]}
	if l.TrackID == "" {
		return Listen{}
	}
	if v, err := strconv.ParseInt(m["progress_ms"], 10, 32); err == nil {
		p := int32(v)
		l.ProgressMS = &p
	}
	l.Counted, l.StartedAt, l.AppliedAt, l.LastSeen = true, now, now, now
	return l
}
