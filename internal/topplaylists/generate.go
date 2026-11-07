package topplaylists

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
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
// waiting_auth).
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

	_, aerr := r.attempt(ctx, p, rec, s, e, loc, now)
	stored, lerr := r.Store.TopPlaylist(ctx, string(p), s)
	if lerr != nil {
		return rec, errors.Join(aerr, lerr)
	}
	return stored, aerr
}
