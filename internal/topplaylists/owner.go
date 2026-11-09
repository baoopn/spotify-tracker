package topplaylists

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// Owner name sources reported to the admin.
const (
	OwnerSourceConfig  = "config"  // TOP_PLAYLISTS_OWNER
	OwnerSourceSpotify = "spotify" // the connected account's display name
	OwnerSourceNone    = "none"    // unresolved: names have no owner
)

const (
	// ownerCacheTTL is how long a display name read from Spotify is reused.
	ownerCacheTTL = 24 * time.Hour
	// renameBatch bounds the playlists renamed per tick.
	renameBatch = 5
)

// ownerState is the owner used by the current tick. resolved is false when
// TOP_PLAYLISTS_OWNER is empty and the Spotify name could not be read (and
// was never read before): new playlists then get the plain name and
// existing ones keep theirs, so a transient failure never renames anything.
type ownerState struct {
	name      string
	source    string
	resolved  bool
	fetchedAt time.Time // of the Spotify name
}

// resolveOwner sets the owner for this tick: the configured one, else the
// cached Spotify display name (read again once it is a day old; on a
// failure the older name keeps being used).
func (r *Runner) resolveOwner(ctx context.Context, now time.Time) {
	if r.Config.Owner != "" {
		r.setOwner(ownerState{name: r.Config.Owner, source: OwnerSourceConfig, resolved: true})
		return
	}
	cur := r.ownerSnapshot()
	if cur.source == OwnerSourceSpotify && now.Sub(cur.fetchedAt) < ownerCacheTTL {
		return
	}
	name, err := r.Spotify.CurrentUserName(ctx)
	if err == nil {
		name, _ = ParseOwner(name)
	}
	switch {
	case err == nil && name != "":
		r.setOwner(ownerState{name: name, source: OwnerSourceSpotify, resolved: true, fetchedAt: now})
	case cur.source == OwnerSourceSpotify:
		r.Log.WarnContext(ctx, "read spotify display name failed; using the cached one", logging.Err(err))
	default:
		if err != nil {
			r.Log.WarnContext(ctx, "read spotify display name failed; playlist names have no owner this tick",
				logging.Err(err))
		}
		r.setOwner(ownerState{source: OwnerSourceNone})
	}
}

func (r *Runner) setOwner(s ownerState) {
	r.ownerMu.Lock()
	defer r.ownerMu.Unlock()
	r.owner = s
}

func (r *Runner) ownerSnapshot() ownerState {
	r.ownerMu.Lock()
	defer r.ownerMu.Unlock()
	return r.owner
}

// Owner returns the owner name used in playlist names ("" when unresolved)
// and its source (OwnerSourceConfig, OwnerSourceSpotify or OwnerSourceNone).
func (r *Runner) Owner() (name, source string) {
	if r.Config.Owner != "" {
		return r.Config.Owner, OwnerSourceConfig
	}
	s := r.ownerSnapshot()
	if !s.resolved {
		return "", OwnerSourceNone
	}
	return s.name, s.source
}

// nameFor is the playlist name of the period starting at start: the owner's
// name, or with no resolved owner the current name (plain for a new row).
func (r *Runner) nameFor(p Period, start time.Time, current string) string {
	s := r.ownerSnapshot()
	if r.Config.Owner != "" {
		s = ownerState{name: r.Config.Owner, resolved: true}
	}
	if !s.resolved {
		if current != "" {
			return current
		}
		return Name(p, start)
	}
	return OwnerName(s.name, p, start)
}

// rowLoc is the zone a row's period was generated in.
func (r *Runner) rowLoc(rec domain.TopPlaylistRecord) *time.Location {
	if rec.TimeZone != "" {
		if l, err := time.LoadLocation(rec.TimeZone); err == nil {
			return l
		}
	}
	if r.Config.Location != nil {
		return r.Config.Location
	}
	return time.UTC
}

// descriptionFor is the Spotify description a created row should carry.
func (r *Runner) descriptionFor(rec domain.TopPlaylistRecord, start time.Time) string {
	if rec.Live {
		return LiveDescription(Period(rec.Period), start, r.Config.Refresh())
	}
	return Description(Period(rec.Period), start)
}

// rename gives created playlists whose stored name differs from the current
// owner's name the new name (a few per tick). It does nothing while the
// owner is unresolved. A failure records last_error and is retried on the
// next tick; a playlist that no longer exists is restored. It reports
// whether Spotify rate-limited a request.
func (r *Runner) rename(ctx context.Context, now time.Time) bool {
	if !r.ownerSnapshot().resolved && r.Config.Owner == "" {
		return false
	}
	if msg, err := r.authProblem(ctx); err != nil || msg != "" {
		return false // nothing can be renamed until Spotify is authorized
	}
	rows, err := r.Store.AllCreatedTopPlaylists(ctx)
	if err != nil {
		r.Log.WarnContext(ctx, "list top playlists to rename failed", logging.Err(err))
		return false
	}
	done := 0
	for _, rec := range rows {
		if done >= renameBatch {
			break
		}
		if rec.SpotifyPlaylistID == "" || !r.Config.Has(Period(rec.Period)) {
			continue
		}
		start := rec.PeriodStart.In(r.rowLoc(rec))
		name := r.nameFor(Period(rec.Period), start, rec.Name)
		if name == rec.Name {
			continue
		}
		done++
		rec.Name = name // a restore recreates the playlist under the new name
		err := r.Spotify.UpdatePlaylistDetails(ctx, rec.SpotifyPlaylistID, name, r.descriptionFor(rec, start))
		switch {
		case err == nil:
			rec.LastError = ""
			if err := r.upsert(ctx, rec); err != nil {
				r.Log.WarnContext(ctx, "save renamed top playlist failed", slog.String("name", name), logging.Err(err))
				continue
			}
			r.Log.InfoContext(ctx, "top playlist renamed", slog.String("name", name),
				slog.String("spotify_id", rec.SpotifyPlaylistID))
			if r.Invalidate != nil {
				r.Invalidate(ctx)
			}
		case errors.Is(err, spotify.ErrNotFound):
			if _, rerr := r.restore(ctx, rec, now); isRateLimit(rerr) {
				return true
			}
		case isRateLimit(err):
			return true
		default:
			msg := "rename: " + err.Error()
			if m, _, ok := authMessage(err); ok {
				msg = m
			}
			r.setError(ctx, rec, msg)
			r.Log.WarnContext(ctx, "top playlist rename failed; retried next tick",
				slog.String("name", name), logging.Err(err))
		}
	}
	return false
}

// setError stores a row's last error only.
func (r *Runner) setError(ctx context.Context, rec domain.TopPlaylistRecord, msg string) {
	wctx, cancel := storeCtx(ctx)
	defer cancel()
	if err := r.Store.SetTopPlaylistError(wctx, rec.Period, rec.PeriodStart, truncateError(msg)); err != nil {
		r.Log.WarnContext(ctx, "save top playlist error failed", slog.String("name", rec.Name), logging.Err(err))
	}
}
