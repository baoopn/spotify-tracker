package topplaylists

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// Errors returned by Generate and RunNow.
var (
	// ErrBusy means another instance or tick holds the generator lock.
	ErrBusy = errors.New("top playlists: a generation is already running")
	// ErrNotCompleted means the period is still running or in the future.
	ErrNotCompleted = errors.New("top playlists: the period has not ended yet")
	// ErrMisaligned means start is not the first instant of a period (it
	// also matches ErrNotCompleted).
	ErrMisaligned = fmt.Errorf("%w: start is not the beginning of a period", ErrNotCompleted)
	// ErrPeriodDisabled means the period kind is not in TOP_PLAYLISTS_PERIODS.
	ErrPeriodDisabled = errors.New("top playlists: period kind is not enabled")
	// ErrNotAuthorized means the row is now waiting_auth: Spotify is not
	// connected or lacks playlist-modify-public. The Spotify cause, if any,
	// is wrapped too (errors.Is spotify.ErrForbidden etc.).
	ErrNotAuthorized = errors.New("top playlists: spotify is not authorized to write playlists")
	// ErrNoPlays means a published playlist's period now ranks no tracks; the
	// playlist and its snapshot are left unchanged.
	ErrNoPlays = errors.New("top playlists: no plays in this period; playlist left unchanged")
)

// Generate forces generation of the completed period of kind p that starts
// at start (any older period may be backfilled). Unlike a tick it ignores
// the created/empty/backoff skip rules: it re-ranks the period, replaces the
// stored snapshot and creates the Spotify playlist if the row has none,
// otherwise it replaces the items of the same playlist (never a duplicate).
//
// start must be the beginning of a period in the configured zone, or in the
// row's own zone when the row exists, and the period must have ended. The
// stored row is returned after the attempt, together with the attempt's
// error: failures are stored exactly like a tick stores them (failed or
// waiting_auth), except for a published (created) row, which is never
// unpublished (see refreshPublished). A created live row of the ended period
// is finalized in place (see finalize).
func (r *Runner) Generate(ctx context.Context, p Period, start time.Time) (domain.TopPlaylistRecord, error) {
	if !r.Config.Enabled() || !r.Config.Has(p) {
		return domain.TopPlaylistRecord{}, ErrPeriodDisabled
	}
	unlock, ok, err := r.Store.LockTopPlaylists(ctx)
	if err != nil {
		return domain.TopPlaylistRecord{}, err
	}
	if !ok {
		return domain.TopPlaylistRecord{}, ErrBusy
	}
	defer unlock()

	loc := r.Config.Location
	rec, err := r.Store.TopPlaylist(ctx, string(p), start)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		rec = domain.TopPlaylistRecord{Period: string(p), PeriodStart: start}
	case err != nil:
		return domain.TopPlaylistRecord{}, err
	case rec.TimeZone != "":
		if l, err := time.LoadLocation(rec.TimeZone); err == nil {
			loc = l // keep an existing row in the zone it was generated in
		}
	}
	s, e := Bounds(p, start, loc)
	if !s.Equal(start) {
		return domain.TopPlaylistRecord{}, ErrMisaligned
	}
	now := r.clock()
	if e.After(now) {
		return domain.TopPlaylistRecord{}, ErrNotCompleted
	}
	if err := r.checkPrunable(ctx, p, rec, s); err != nil {
		return domain.TopPlaylistRecord{}, err
	}

	// Store writes outlive the request timeout so the outcome is always
	// recorded. create's own detached 30 s bound may outlive the admin
	// router's timeout too; that is safe because the new playlist ID is saved
	// before anything else, so a retry never creates a duplicate.
	ctx = context.WithValue(ctx, detachedWritesKey{}, true)
	r.resolveOwner(ctx, now)
	var aerr error
	switch {
	case rec.Status == domain.TopPlaylistCreated && rec.SpotifyPlaylistID != "" && rec.Live:
		_, aerr = r.finalize(ctx, p, rec, s, e, loc, now) // forced: ignores the finalize backoff
	case rec.Status == domain.TopPlaylistCreated && rec.SpotifyPlaylistID != "":
		aerr = r.refreshPublished(ctx, p, rec, s, e, loc, now)
	default:
		_, aerr = r.attempt(ctx, p, rec, s, e, loc, now, false)
	}
	rctx, cancel := storeCtx(ctx)
	defer cancel()
	stored, lerr := r.Store.TopPlaylist(rctx, string(p), s)
	if lerr != nil {
		return rec, errors.Join(aerr, lerr)
	}
	return stored, aerr
}

// refreshPublished regenerates a created row without ever unpublishing it:
// nothing is written when Spotify is not authorized or the period now ranks
// no tracks; on a failure only last_error is recorded (status, published_at,
// attempts and next_attempt_at are kept). The new snapshot is saved only
// after Spotify accepted the new items, so it always matches the playlist.
func (r *Runner) refreshPublished(ctx context.Context, p Period, rec domain.TopPlaylistRecord, start, end time.Time,
	loc *time.Location, now time.Time) error {
	msg, err := r.authProblem(ctx)
	if err != nil {
		return err
	}
	if msg != "" {
		return fmt.Errorf("%w: %s", ErrNotAuthorized, msg)
	}
	tracks, err := r.Store.RankTracks(ctx, start, end, TrackLimit)
	if err != nil {
		return r.keepPublished(ctx, rec, err.Error(), err)
	}
	if len(tracks) == 0 {
		return ErrNoPlays
	}

	uris := make([]string, len(tracks))
	for i, t := range tracks {
		uris[i] = "spotify:track:" + t.TrackID
	}
	err = r.Spotify.ReplacePlaylistItems(ctx, rec.SpotifyPlaylistID, uris)
	if errors.Is(err, spotify.ErrNotFound) { // deleted outside: restore, then retry once
		restored, rerr := r.restore(ctx, rec, now)
		if rerr != nil {
			return rerr
		}
		rec = restored
		err = r.Spotify.ReplacePlaylistItems(ctx, rec.SpotifyPlaylistID, uris)
	}
	if err != nil {
		if m, _, ok := authMessage(err); ok {
			return r.keepPublished(ctx, rec, m, fmt.Errorf("%w: %s: %w", ErrNotAuthorized, m, err))
		}
		return r.keepPublished(ctx, rec, err.Error(), err)
	}

	next := rec
	next.PeriodStart, next.PeriodEnd, next.TimeZone = start, end, loc.String()
	next.Name = r.nameFor(p, start, rec.Name)
	next.TrackCount, next.TotalPlays = len(tracks), 0
	for _, t := range tracks {
		next.TotalPlays += t.Plays
	}
	published := now.UTC()
	next.Status, next.PublishedAt, next.Attempts = domain.TopPlaylistCreated, &published, 0
	next.LastError, next.NextAttemptAt = "", nil
	if err := r.saveSnapshot(ctx, next, tracks); err != nil {
		return r.keepPublished(ctx, rec, "save snapshot: "+err.Error(), err)
	}
	r.Log.InfoContext(ctx, "top playlist regenerated", slog.String("name", next.Name),
		slog.String("spotify_id", next.SpotifyPlaylistID), slog.Int("tracks", next.TrackCount))
	if r.Invalidate != nil {
		r.Invalidate(ctx)
	}
	return nil
}

// keepPublished records a failed regeneration of a published playlist: only
// last_error changes, so the row stays public and the tick skip rules hold.
func (r *Runner) keepPublished(ctx context.Context, rec domain.TopPlaylistRecord, msg string, cause error) error {
	rec.LastError = truncateError(msg)
	if err := r.upsert(ctx, rec); err != nil {
		r.Log.WarnContext(ctx, "save top playlist state failed", slog.String("name", rec.Name), logging.Err(err))
		return errors.Join(cause, err)
	}
	r.Log.WarnContext(ctx, "top playlist regeneration failed; published playlist kept",
		slog.String("name", rec.Name), logging.Err(cause))
	return cause
}

// detachedWritesKey marks a context whose store writes must survive its
// cancellation (forced generation from the admin).
type detachedWritesKey struct{}

// writeTimeout bounds a detached store write.
const writeTimeout = 10 * time.Second

// storeCtx returns the context for a store write: ctx itself on a tick, or a
// copy detached from cancellation (bounded by writeTimeout) under Generate.
func storeCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Value(detachedWritesKey{}) == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
}

func (r *Runner) upsert(ctx context.Context, rec domain.TopPlaylistRecord) error {
	wctx, cancel := storeCtx(ctx)
	defer cancel()
	return r.Store.UpsertTopPlaylist(wctx, rec)
}

func (r *Runner) saveSnapshot(ctx context.Context, rec domain.TopPlaylistRecord, tracks []domain.RankedTrack) error {
	wctx, cancel := storeCtx(ctx)
	defer cancel()
	return r.Store.SaveTopPlaylistSnapshot(wctx, rec, tracks)
}
