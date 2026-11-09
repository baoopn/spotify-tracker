package topplaylists

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
)

// pruneBatch bounds the playlists pruned, and the Spotify removals tried,
// per tick.
const pruneBatch = 10

// Errors returned by Generate and Regenerate.
var (
	// ErrTooOld means the period would be pruned right away: its kind
	// already has Config.Keep newer playlists.
	ErrTooOld = errors.New("top playlists: only the newest playlists per kind are kept")
	// ErrPruned means the row was pruned; it is history only.
	ErrPruned = errors.New("top playlists: this playlist was pruned")
)

// prune keeps the newest Config.Keep created playlists of every enabled
// kind (the live one counts) and prunes the older ones, oldest first. A row
// is marked pruned before Spotify is asked to remove the playlist, so a
// restore never brings it back; removals that fail are retried on later
// ticks. It reports whether Spotify rate-limited a request.
func (r *Runner) prune(ctx context.Context, now time.Time) bool {
	if r.Config.Keep <= 0 {
		return false
	}
	budget := pruneBatch
	for _, p := range r.Config.Periods {
		rows, err := r.Store.CreatedTopPlaylists(ctx, string(p))
		if err != nil {
			r.Log.WarnContext(ctx, "list top playlists to prune failed", slog.String("period", string(p)), logging.Err(err))
			continue
		}
		for i := len(rows) - 1; i >= r.Config.Keep && budget > 0; i-- { // oldest first
			rec := rows[i]
			if err := r.Store.PruneTopPlaylist(ctx, rec.Period, rec.PeriodStart, now.UTC()); err != nil {
				r.Log.WarnContext(ctx, "prune top playlist failed", slog.String("name", rec.Name), logging.Err(err))
				continue
			}
			budget--
			r.Log.InfoContext(ctx, "top playlist pruned", slog.String("name", rec.Name),
				slog.String("spotify_id", rec.SpotifyPlaylistID), slog.Int("keep", r.Config.Keep))
			if r.Invalidate != nil {
				r.Invalidate(ctx)
			}
		}
	}
	return r.unfollowPruned(ctx)
}

// unfollowPruned removes pruned playlists from Spotify (a few per tick,
// oldest first) until Spotify confirms each removal. It reports whether
// Spotify rate-limited a request.
func (r *Runner) unfollowPruned(ctx context.Context) bool {
	rows, err := r.Store.PendingUnfollows(ctx, pruneBatch)
	if err != nil {
		r.Log.WarnContext(ctx, "list pruned top playlists failed", logging.Err(err))
		return false
	}
	if len(rows) == 0 {
		return false
	}
	msg, err := r.authProblem(ctx)
	if err != nil {
		r.Log.WarnContext(ctx, "read spotify authorization failed", logging.Err(err))
		return false
	}
	for _, rec := range rows {
		if msg != "" {
			if rec.LastError != msg {
				r.setError(ctx, rec, msg)
			}
			continue
		}
		err := r.Spotify.UnfollowPlaylist(ctx, rec.SpotifyPlaylistID)
		switch {
		case err == nil:
			if err := r.Store.MarkSpotifyRemoved(ctx, rec.Period, rec.PeriodStart); err != nil {
				r.Log.WarnContext(ctx, "save pruned top playlist removal failed", slog.String("name", rec.Name), logging.Err(err))
				continue
			}
			r.Log.InfoContext(ctx, "pruned top playlist removed from spotify", slog.String("name", rec.Name),
				slog.String("spotify_id", rec.SpotifyPlaylistID))
		case isRateLimit(err):
			r.setError(ctx, rec, "remove from spotify: "+err.Error())
			return true
		default:
			m := "remove from spotify: " + err.Error()
			if am, _, ok := authMessage(err); ok {
				m = am
			}
			r.setError(ctx, rec, m)
			r.Log.WarnContext(ctx, "remove pruned top playlist from spotify failed; retried next tick",
				slog.String("name", rec.Name), logging.Err(err))
		}
	}
	return false
}

// tooOld reports whether a period of kind p starting at start would be
// pruned right away because its kind already has Config.Keep newer created
// playlists.
func (r *Runner) tooOld(ctx context.Context, p Period, start time.Time) (bool, error) {
	if r.Config.Keep <= 0 {
		return false, nil
	}
	rows, err := r.Store.CreatedTopPlaylists(ctx, string(p))
	if err != nil {
		return false, err
	}
	newer := 0
	for _, rec := range rows {
		if rec.PeriodStart.After(start) {
			newer++
		}
	}
	return newer >= r.Config.Keep, nil
}

// checkPrunable rejects forced generation of a pruned row (ErrPruned) and of
// a period that would be pruned right away (ErrTooOld).
func (r *Runner) checkPrunable(ctx context.Context, p Period, rec domain.TopPlaylistRecord, start time.Time) error {
	if rec.Status == domain.TopPlaylistPruned {
		return ErrPruned
	}
	if rec.Status == domain.TopPlaylistCreated {
		return nil // a kept playlist
	}
	old, err := r.tooOld(ctx, p, start)
	if err != nil {
		return err
	}
	if old {
		return ErrTooOld
	}
	return nil
}
