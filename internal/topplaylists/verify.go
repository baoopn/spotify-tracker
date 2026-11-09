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
	// restoreBatch bounds the playlists restored per check.
	restoreBatch = 3
	// freshAge is how long after a publish or restore a playlist is not
	// checked: Spotify may not list a new playlist right away.
	freshAge = 15 * time.Minute
)

// fresh reports whether rec was published or restored less than freshAge ago.
func fresh(rec domain.TopPlaylistRecord, now time.Time) bool {
	for _, t := range []*time.Time{rec.PublishedAt, rec.RestoredAt} {
		if t != nil && now.Sub(*t) < freshAge {
			return true
		}
	}
	return false
}

// Verify checks the published playlists against Spotify now, ignoring
// TOP_PLAYLISTS_VERIFY, and restores the ones deleted outside this system.
// It returns ErrBusy when another instance or tick holds the lock.
func (r *Runner) Verify(ctx context.Context) error {
	unlock, ok, err := r.Store.LockTopPlaylists(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrBusy
	}
	defer unlock()
	now := r.clock()
	ctx = context.WithValue(ctx, detachedWritesKey{}, true) // see Generate
	r.resolveOwner(ctx, now)
	r.verify(ctx, now, true)
	return nil
}

// verify checks every created playlist (hidden and live ones included)
// against the owner's Spotify library once per Config.Verify (always when
// forced) and restores the missing ones, a few per check. When the library
// cannot be read, or reads as empty while playlists are published, nothing
// is changed. It reports whether Spotify rate-limited a request.
func (r *Runner) verify(ctx context.Context, now time.Time, force bool) bool {
	if !force {
		if r.Config.Verify <= 0 {
			return false
		}
		last, err := r.Store.LastTopPlaylistVerify(ctx)
		if err != nil {
			r.Log.WarnContext(ctx, "read last top playlist check failed", logging.Err(err))
			return false
		}
		if last != nil && now.Sub(*last) < r.Config.Verify {
			return false
		}
	}
	rows, err := r.Store.AllCreatedTopPlaylists(ctx)
	if err != nil {
		r.Log.WarnContext(ctx, "list top playlists to check failed", logging.Err(err))
		return false
	}
	if len(rows) == 0 {
		return false
	}
	if msg, err := r.authProblem(ctx); err != nil || msg != "" {
		return false
	}
	owned, err := r.Spotify.OwnedPlaylistIDs(ctx)
	if err != nil {
		r.Log.WarnContext(ctx, "list spotify playlists failed; top playlists not checked", logging.Err(err))
		return isRateLimit(err)
	}
	if len(owned) == 0 {
		r.Log.WarnContext(ctx, "spotify lists no playlists while top playlists are published; not restoring anything",
			slog.Int("published", len(rows)))
		return false
	}

	// Playlists published (or restored) moments ago may not be listed yet.
	var checked []domain.TopPlaylistRecord
	present := 0
	for _, rec := range rows {
		if rec.SpotifyPlaylistID == "" || fresh(rec, now) {
			continue
		}
		checked = append(checked, rec)
		if owned[rec.SpotifyPlaylistID] {
			present++
		}
	}
	if present == 0 && len(checked) >= 2 {
		r.Log.WarnContext(ctx, "none of the top playlists is in the Spotify library (another account?); not restoring anything",
			slog.Int("published", len(checked)))
		return false
	}

	limited, restores := false, 0
	keys := make([]domain.TopPlaylistKey, 0, len(checked))
	for _, rec := range checked {
		keys = append(keys, domain.TopPlaylistKey{Period: rec.Period, Start: rec.PeriodStart})
		if owned[rec.SpotifyPlaylistID] || restores >= restoreBatch || limited {
			continue
		}
		restores++
		if _, err := r.restore(ctx, rec, now); isRateLimit(err) {
			limited = true
		}
	}
	wctx, cancel := storeCtx(ctx)
	defer cancel()
	if err := r.Store.MarkVerified(wctx, keys, now.UTC()); err != nil {
		r.Log.WarnContext(ctx, "save top playlist check failed", logging.Err(err))
	}
	return limited
}

// restore brings back a created playlist that is gone from the owner's
// library: it is followed again (and its items reset to the stored
// snapshot), or recreated under a new ID when Spotify no longer has it.
// The row stays created either way; a failure only records last_error and
// is retried by the next check. The stored row is returned.
func (r *Runner) restore(ctx context.Context, rec domain.TopPlaylistRecord, now time.Time) (domain.TopPlaylistRecord, error) {
	err := r.Spotify.FollowPlaylist(ctx, rec.SpotifyPlaylistID)
	how := "re-followed"
	if errors.Is(err, spotify.ErrNotFound) {
		how = "recreated"
		start := rec.PeriodStart.In(r.rowLoc(rec))
		rec.Name = r.nameFor(Period(rec.Period), start, rec.Name)
		rec, err = r.recreate(ctx, rec, r.descriptionFor(rec, start))
	}
	if err == nil {
		err = r.restoreItems(ctx, rec)
	}
	if err != nil {
		msg := "restore failed: " + err.Error()
		if m, _, ok := authMessage(err); ok {
			msg = "restore failed: " + m
		}
		r.setError(ctx, rec, msg)
		r.Log.WarnContext(ctx, "top playlist restore failed; retried next check",
			slog.String("name", rec.Name), slog.String("spotify_id", rec.SpotifyPlaylistID), logging.Err(err))
		return rec, err
	}
	restored := now.UTC()
	rec.RestoredAt, rec.LastError = &restored, ""
	if err := r.upsert(ctx, rec); err != nil {
		r.Log.WarnContext(ctx, "save restored top playlist failed", slog.String("name", rec.Name), logging.Err(err))
		return rec, err
	}
	r.Log.InfoContext(ctx, "top playlist restored ("+how+")", slog.String("name", rec.Name),
		slog.String("spotify_id", rec.SpotifyPlaylistID))
	if r.Invalidate != nil {
		r.Invalidate(ctx)
	}
	return rec, nil
}

// recreate makes a new Spotify playlist for a row whose playlist is gone and
// saves its ID right away (see create, which also keeps an unsaved ID so a
// retry never makes a second one).
func (r *Runner) recreate(ctx context.Context, rec domain.TopPlaylistRecord, description string) (domain.TopPlaylistRecord, error) {
	if _, pending := r.unsavedPlaylist(keyOf(rec)); !pending {
		rec.SpotifyPlaylistID, rec.SpotifyURL = "", ""
	}
	out, err := r.create(ctx, rec, description)
	if err != nil {
		return out, fmt.Errorf("recreate playlist: %w", err)
	}
	return out, nil
}

// restoreItems puts the stored snapshot back into the row's playlist.
func (r *Runner) restoreItems(ctx context.Context, rec domain.TopPlaylistRecord) error {
	tracks, err := r.Store.TopPlaylistSnapshot(ctx, rec.Period, rec.PeriodStart)
	if err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	if len(tracks) == 0 {
		return nil
	}
	return r.Spotify.ReplacePlaylistItems(ctx, rec.SpotifyPlaylistID, trackURIs(tracks))
}
