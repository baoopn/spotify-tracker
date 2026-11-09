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

const (
	// FinalizeGrace is how long after its end a live period is finalized,
	// so plays recorded late still count. Forced finalization ignores it.
	FinalizeGrace = 15 * time.Minute
	// finalizeBatch bounds the ended live playlists finalized per tick.
	finalizeBatch = 3
)

// finalizeEnded finalizes created live playlists whose period ended at least
// FinalizeGrace ago (oldest first, a few per tick), including ones a long
// outage left behind. It reports whether Spotify rate-limited a request.
func (r *Runner) finalizeEnded(ctx context.Context, now time.Time) bool {
	rows, err := r.Store.EndedLiveTopPlaylists(ctx, now.Add(-FinalizeGrace), now, finalizeBatch)
	if err != nil {
		r.Log.WarnContext(ctx, "list ended live top playlists failed", logging.Err(err))
		return false
	}
	for _, rec := range rows {
		loc := r.Config.Location
		if l, err := time.LoadLocation(rec.TimeZone); err == nil && rec.TimeZone != "" {
			loc = l // finalize in the zone the period was generated in
		}
		if loc == nil {
			loc = time.UTC
		}
		p := Period(rec.Period)
		if limited, _ := r.finalize(ctx, p, rec, rec.PeriodStart.In(loc), rec.PeriodEnd.In(loc), loc, now); limited {
			return true
		}
	}
	return false
}

// live advances the current period's live playlist on a tick: it is created
// on the period's first plays, then re-ranked every Config.Refresh(). A live
// row whose creation failed is retried like any other attempt. It reports
// whether Spotify rate-limited the request.
func (r *Runner) live(ctx context.Context, p Period, now time.Time) bool {
	loc := r.Config.Location
	start, end := Bounds(p, now, loc)
	rec, err := r.Store.TopPlaylist(ctx, string(p), start)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		rec = domain.TopPlaylistRecord{Period: string(p), PeriodStart: start}
	case err != nil:
		r.Log.WarnContext(ctx, "load live top playlist failed", slog.String("period", string(p)), logging.Err(err))
		return false
	}
	switch rec.Status {
	case domain.TopPlaylistCreated:
		switch {
		case !rec.Live,
			rec.NextAttemptAt != nil && rec.NextAttemptAt.After(now), // a failed refresh waits
			rec.RefreshedAt != nil && now.Sub(*rec.RefreshedAt) < r.Config.Refresh():
			return false
		}
		limited, _ := r.refreshLive(ctx, p, rec, start, now) // stored on the row and logged
		return limited
	case domain.TopPlaylistFailed, domain.TopPlaylistWaitingAuth:
		if rec.NextAttemptAt != nil && rec.NextAttemptAt.After(now) {
			return false
		}
	}
	limited, _ := r.attempt(ctx, p, rec, start, end, loc, now, true)
	return limited
}

// refreshLive re-ranks a published live playlist over [start, now) without
// ever unpublishing it. An unchanged order makes no Spotify call (only the
// snapshot's plays are updated); a new order replaces the playlist's items
// first and saves the snapshot after Spotify accepted them. A failure only
// records last_error and next_attempt_at (a full refresh interval later);
// refreshed_at advances on success only.
func (r *Runner) refreshLive(ctx context.Context, p Period, rec domain.TopPlaylistRecord, start, now time.Time) (limited bool, err error) {
	msg, err := r.authProblem(ctx)
	if err != nil {
		r.Log.WarnContext(ctx, "read spotify authorization failed", logging.Err(err))
		return false, err
	}
	if msg != "" {
		return false, r.liveFailed(ctx, rec, msg, fmt.Errorf("%w: %s", ErrNotAuthorized, msg), now)
	}
	tracks, err := r.Store.RankTracks(ctx, start, now, TrackLimit)
	if err != nil {
		return false, r.liveFailed(ctx, rec, err.Error(), err, now)
	}
	if len(tracks) == 0 {
		return false, ErrNoPlays
	}
	old, err := r.Store.TopPlaylistSnapshot(ctx, rec.Period, rec.PeriodStart)
	if err != nil {
		return false, r.liveFailed(ctx, rec, "load snapshot: "+err.Error(), err, now)
	}

	changed := !sameOrder(old, tracks)
	if changed {
		if err := r.Spotify.ReplacePlaylistItems(ctx, rec.SpotifyPlaylistID, trackURIs(tracks)); err != nil {
			if errors.Is(err, spotify.ErrNotFound) { // deleted outside: restore; refreshed next tick
				restored, rerr := r.restore(ctx, rec, now)
				if rerr != nil { // retried one refresh interval later, like other live failures
					return isRateLimit(rerr), r.liveFailed(ctx, restored, "restore failed: "+rerr.Error(), rerr, now)
				}
				return false, nil
			}
			return isRateLimit(err), r.liveSpotifyFailed(ctx, rec, err, now)
		}
	}

	next := rec
	setTotals(&next, tracks)
	refreshed := now.UTC()
	next.RefreshedAt, next.LastError, next.NextAttemptAt = &refreshed, "", nil
	if err := r.saveSnapshot(ctx, next, tracks); err != nil {
		return false, r.liveFailed(ctx, rec, "save snapshot: "+err.Error(), err, now)
	}
	r.Log.InfoContext(ctx, "live top playlist refreshed", slog.String("name", next.Name),
		slog.String("spotify_id", next.SpotifyPlaylistID), slog.Int("tracks", next.TrackCount),
		slog.Bool("reordered", changed))
	if (changed || !samePlays(old, tracks)) && r.Invalidate != nil {
		r.Invalidate(ctx)
	}
	return false, nil
}

// liveSpotifyFailed records a failed Spotify call during a live refresh;
// authorization problems store the re-authorize message.
func (r *Runner) liveSpotifyFailed(ctx context.Context, rec domain.TopPlaylistRecord, err error, now time.Time) error {
	if m, _, ok := authMessage(err); ok {
		return r.liveFailed(ctx, rec, m, fmt.Errorf("%w: %s: %w", ErrNotAuthorized, m, err), now)
	}
	return r.liveFailed(ctx, rec, err.Error(), err, now)
}

// liveFailed records a failed live refresh: the row stays created, live and
// published with its old snapshot; only last_error and next_attempt_at (one
// refresh interval later) change.
func (r *Runner) liveFailed(ctx context.Context, rec domain.TopPlaylistRecord, msg string, cause error, now time.Time) error {
	retry := now.Add(r.Config.Refresh()).UTC()
	rec.LastError, rec.NextAttemptAt = truncateError(msg), &retry
	if err := r.upsert(ctx, rec); err != nil {
		r.Log.WarnContext(ctx, "save top playlist state failed", slog.String("name", rec.Name), logging.Err(err))
		return errors.Join(cause, err)
	}
	r.Log.WarnContext(ctx, "live top playlist refresh failed; published playlist kept",
		slog.String("name", rec.Name), logging.Err(cause))
	return cause
}

// finalize turns a live playlist whose period has ended into the final one,
// in place: it re-ranks the full period [start, end), replaces the items if
// the order changed, sets the final description and clears live. A failure
// keeps the row live and published, records last_error and backs off
// (attempts / next_attempt_at) before a later tick retries.
func (r *Runner) finalize(ctx context.Context, p Period, rec domain.TopPlaylistRecord, start, end time.Time,
	loc *time.Location, now time.Time) (limited bool, err error) {
	msg, err := r.authProblem(ctx)
	if err != nil {
		r.Log.WarnContext(ctx, "read spotify authorization failed", logging.Err(err))
		return false, err
	}
	if msg != "" {
		return false, r.finalizeFailed(ctx, rec, msg, fmt.Errorf("%w: %s", ErrNotAuthorized, msg), now)
	}
	tracks, err := r.Store.RankTracks(ctx, start, end, TrackLimit)
	if err != nil {
		return false, r.finalizeFailed(ctx, rec, err.Error(), err, now)
	}
	old, err := r.Store.TopPlaylistSnapshot(ctx, rec.Period, rec.PeriodStart)
	if err != nil {
		return false, r.finalizeFailed(ctx, rec, "load snapshot: "+err.Error(), err, now)
	}
	if len(tracks) > 0 && !sameOrder(old, tracks) {
		if err := r.Spotify.ReplacePlaylistItems(ctx, rec.SpotifyPlaylistID, trackURIs(tracks)); err != nil {
			return r.finalizeSpotifyError(ctx, rec, err, now)
		}
	}
	name := r.nameFor(p, start, rec.Name)
	if err := r.Spotify.UpdatePlaylistDetails(ctx, rec.SpotifyPlaylistID, name, Description(p, start)); err != nil {
		return r.finalizeSpotifyError(ctx, rec, err, now)
	}

	next := rec
	next.PeriodStart, next.PeriodEnd, next.TimeZone, next.Name = start, end, loc.String(), name
	refreshed := now.UTC()
	next.Live, next.RefreshedAt = false, &refreshed
	next.LastError, next.NextAttemptAt, next.Attempts = "", nil, 0
	if len(tracks) > 0 {
		setTotals(&next, tracks)
		err = r.saveSnapshot(ctx, next, tracks)
	} else {
		err = r.upsert(ctx, next) // cannot normally happen: keep the live snapshot
	}
	if err != nil {
		// Spotify is final already; the next try only repeats idempotent calls.
		return false, r.finalizeFailed(ctx, rec, "save final playlist: "+err.Error(), err, now)
	}
	r.Log.InfoContext(ctx, "live top playlist finalized", slog.String("name", next.Name),
		slog.String("spotify_id", next.SpotifyPlaylistID), slog.Int("tracks", next.TrackCount))
	if r.Invalidate != nil {
		r.Invalidate(ctx)
	}
	return false, nil
}

// finalizeSpotifyError handles a failed Spotify write while finalizing: a
// playlist deleted outside this system is restored (and finalized on a
// later tick); anything else is recorded by finalizeSpotifyFailed.
func (r *Runner) finalizeSpotifyError(ctx context.Context, rec domain.TopPlaylistRecord, err error, now time.Time) (bool, error) {
	if errors.Is(err, spotify.ErrNotFound) {
		restored, rerr := r.restore(ctx, rec, now)
		if rerr != nil { // backs off like other finalize failures
			return isRateLimit(rerr), r.finalizeFailed(ctx, restored, "restore failed: "+rerr.Error(), rerr, now)
		}
		return false, nil
	}
	return isRateLimit(err), r.finalizeSpotifyFailed(ctx, rec, err, now)
}

func (r *Runner) finalizeSpotifyFailed(ctx context.Context, rec domain.TopPlaylistRecord, err error, now time.Time) error {
	if m, _, ok := authMessage(err); ok {
		return r.finalizeFailed(ctx, rec, m, fmt.Errorf("%w: %s: %w", ErrNotAuthorized, m, err), now)
	}
	return r.finalizeFailed(ctx, rec, err.Error(), err, now)
}

// finalizeFailed records a failed finalization: the row stays created, live
// and published; last_error is set and the retry backs off like fail does.
func (r *Runner) finalizeFailed(ctx context.Context, rec domain.TopPlaylistRecord, msg string, cause error, now time.Time) error {
	rec.Attempts++
	delay := Backoff(rec.Attempts)
	var rl *spotify.RateLimitError
	if errors.As(cause, &rl) && rl.RetryAfter > delay {
		delay = rl.RetryAfter
	}
	next := now.Add(delay).UTC()
	rec.LastError, rec.NextAttemptAt = truncateError(msg), &next
	if err := r.upsert(ctx, rec); err != nil {
		r.Log.WarnContext(ctx, "save top playlist state failed", slog.String("name", rec.Name), logging.Err(err))
		return errors.Join(cause, err)
	}
	r.Log.WarnContext(ctx, "live top playlist finalization failed; published playlist kept",
		slog.String("name", rec.Name), slog.Int("attempts", rec.Attempts),
		slog.Time("next_attempt_at", next), logging.Err(cause))
	return cause
}

// Regenerate is the admin "regenerate" of an existing row: for a completed
// period it is Generate (which finalizes a live row); for the running period
// of a live kind it forces a live refresh now, ignoring the refresh interval
// (or retries the live creation). A running period of another kind returns
// ErrNotCompleted.
func (r *Runner) Regenerate(ctx context.Context, p Period, start time.Time) (domain.TopPlaylistRecord, error) {
	if !r.Config.Enabled() || !r.Config.Has(p) {
		return domain.TopPlaylistRecord{}, ErrPeriodDisabled
	}
	loc := r.Config.Location
	s, e := Bounds(p, start, loc)
	now := r.clock()
	if !s.Equal(start) || !e.After(now) || !r.Config.IsLive(p) {
		return r.Generate(ctx, p, start) // completed, misaligned or not live
	}
	if s.After(now) {
		return domain.TopPlaylistRecord{}, ErrNotCompleted // future
	}

	unlock, ok, err := r.Store.LockTopPlaylists(ctx)
	if err != nil {
		return domain.TopPlaylistRecord{}, err
	}
	if !ok {
		return domain.TopPlaylistRecord{}, ErrBusy
	}
	defer unlock()

	rec, err := r.Store.TopPlaylist(ctx, string(p), s)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		rec = domain.TopPlaylistRecord{Period: string(p), PeriodStart: s}
	case err != nil:
		return domain.TopPlaylistRecord{}, err
	}
	if rec.Status == domain.TopPlaylistPruned {
		return domain.TopPlaylistRecord{}, ErrPruned
	}
	ctx = context.WithValue(ctx, detachedWritesKey{}, true) // see Generate
	r.resolveOwner(ctx, now)
	var aerr error
	if rec.Status == domain.TopPlaylistCreated && rec.SpotifyPlaylistID != "" {
		_, aerr = r.refreshLive(ctx, p, rec, s, now)
	} else {
		_, aerr = r.attempt(ctx, p, rec, s, e, loc, now, true)
	}
	rctx, cancel := storeCtx(ctx)
	defer cancel()
	stored, lerr := r.Store.TopPlaylist(rctx, string(p), s)
	switch {
	case errors.Is(lerr, domain.ErrNotFound):
		return domain.TopPlaylistRecord{}, aerr // nothing to publish yet
	case lerr != nil:
		return rec, errors.Join(aerr, lerr)
	}
	return stored, aerr
}

// sameOrder reports whether two rankings list the same tracks in the same order.
func sameOrder(a, b []domain.RankedTrack) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].TrackID != b[i].TrackID {
			return false
		}
	}
	return true
}

// samePlays reports whether two rankings of the same order have equal plays.
func samePlays(a, b []domain.RankedTrack) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Plays != b[i].Plays {
			return false
		}
	}
	return true
}

func trackURIs(tracks []domain.RankedTrack) []string {
	uris := make([]string, len(tracks))
	for i, t := range tracks {
		uris[i] = "spotify:track:" + t.TrackID
	}
	return uris
}

func setTotals(rec *domain.TopPlaylistRecord, tracks []domain.RankedTrack) {
	rec.TrackCount, rec.TotalPlays = len(tracks), 0
	for _, t := range tracks {
		rec.TotalPlays += t.Plays
	}
}

func isRateLimit(err error) bool {
	var rl *spotify.RateLimitError
	return errors.As(err, &rl)
}
