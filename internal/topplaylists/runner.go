package topplaylists

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

// Messages stored in last_error while waiting for (re-)authorization.
const (
	msgMissingScope = "re-authorize Spotify in the admin to grant " + spotify.PlaylistWriteScope
	msgNotConnected = "connect Spotify in the admin to grant " + spotify.PlaylistWriteScope
	msgRevoked      = "the Spotify authorization expired or was revoked; re-authorize in the admin"
)

const (
	maxErrorLen = 300
	baseBackoff = 5 * time.Minute
	maxBackoff  = 6 * time.Hour
	// forbiddenRetry spaces out attempts after Spotify answered 403 even though
	// the stored scopes look right (e.g. the account is not allow-listed).
	forbiddenRetry = time.Hour
	// createTimeout bounds a playlist create plus saving its ID. It runs on a
	// context that ignores shutdown, so a create that reached Spotify is
	// always recorded and never repeated.
	createTimeout = 30 * time.Second
	// DefaultInterval is the time between two generator ticks.
	DefaultInterval = 10 * time.Minute
)

// Store is the persistence the runner needs (implemented by *store.Store).
type Store interface {
	TopPlaylist(ctx context.Context, period string, start time.Time) (domain.TopPlaylistRecord, error)
	UpsertTopPlaylist(ctx context.Context, p domain.TopPlaylistRecord) error
	SaveTopPlaylistSnapshot(ctx context.Context, p domain.TopPlaylistRecord, tracks []domain.RankedTrack) error
	// TopPlaylistSnapshot returns the stored ranking in position order.
	TopPlaylistSnapshot(ctx context.Context, period string, start time.Time) ([]domain.RankedTrack, error)
	RankTracks(ctx context.Context, start, end time.Time, limit int) ([]domain.RankedTrack, error)
	// LockTopPlaylists takes a cluster-wide lock so overlapping instances
	// (e.g. during a rolling deploy) never generate at the same time.
	LockTopPlaylists(ctx context.Context) (unlock func(), ok bool, err error)
}

// Spotify writes playlists (implemented by *spotify.Manager).
type Spotify interface {
	CreatePlaylist(ctx context.Context, name, description string, public bool) (id, url string, err error)
	ReplacePlaylistItems(ctx context.Context, id string, uris []string) error
	UpdatePlaylistDetails(ctx context.Context, id, name, description string) error
}

// Auth reads the stored Spotify authorization (implemented by *settings.Store).
type Auth interface {
	Spotify(ctx context.Context) (settings.Spotify, error)
}

// Runner creates the top playlist of the most recently completed period of
// every enabled kind, and keeps the live playlist of the current period of
// every live kind up to date (finalizing it in place once the period ends).
// Each period's row in top_playlists is its state, so the runner is safe to
// restart at any point and never creates duplicates.
type Runner struct {
	Config  Config
	Store   Store
	Spotify Spotify
	Auth    Auth
	Log     *slog.Logger
	// Invalidate drops cached public API responses after a playlist is published.
	Invalidate func(ctx context.Context)
	FirstDelay time.Duration // before the first tick
	Interval   time.Duration // between ticks

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) bool
}

// New returns a Runner with production timings: first tick a minute after
// start, then every 10 minutes.
func New(cfg Config, st Store, sp Spotify, auth Auth, invalidate func(context.Context), log *slog.Logger) *Runner {
	return &Runner{Config: cfg, Store: st, Spotify: sp, Auth: auth, Invalidate: invalidate, Log: log,
		FirstDelay: time.Minute, Interval: DefaultInterval}
}

// Run ticks until ctx is done.
func (r *Runner) Run(ctx context.Context) error {
	if !r.wait(ctx, r.FirstDelay) {
		return nil
	}
	for {
		r.RunOnce(ctx)
		if !r.wait(ctx, r.Interval) {
			return nil
		}
	}
}

// RunOnce handles the last completed period of every enabled kind. It does
// nothing when another instance holds the lock.
func (r *Runner) RunOnce(ctx context.Context) {
	if err := r.RunNow(ctx); err != nil && !errors.Is(err, ErrBusy) {
		r.Log.WarnContext(ctx, "top playlists lock failed", logging.Err(err))
	}
}

// RunNow is RunOnce for callers that need the outcome: it returns ErrBusy
// when another instance or tick holds the lock. Per-period failures are
// stored on their rows (and logged), not returned.
func (r *Runner) RunNow(ctx context.Context) error {
	unlock, ok, err := r.Store.LockTopPlaylists(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrBusy // another instance is generating
	}
	defer unlock()
	now := r.clock()
	for _, p := range r.Config.Periods {
		if rateLimited := r.generate(ctx, p, now); rateLimited {
			return nil // the other kinds would be refused too; try next tick
		}
		if r.Config.IsLive(p) {
			if rateLimited := r.live(ctx, p, now); rateLimited {
				return nil
			}
		}
	}
	return nil
}

// generate advances the last completed period's state machine on a tick:
// finished periods (created, empty) and failures still backing off are
// skipped, except a created live playlist, which is finalized (with its own
// backoff). It reports whether Spotify rate-limited the request.
func (r *Runner) generate(ctx context.Context, p Period, now time.Time) bool {
	loc := r.Config.Location
	start, end := LastCompleted(p, now, loc)
	rec, err := r.Store.TopPlaylist(ctx, string(p), start)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		rec = domain.TopPlaylistRecord{Period: string(p), PeriodStart: start}
	case err != nil:
		r.Log.WarnContext(ctx, "load top playlist failed", slog.String("period", string(p)), logging.Err(err))
		return false
	}
	switch rec.Status {
	case domain.TopPlaylistCreated:
		if !rec.Live || (rec.NextAttemptAt != nil && rec.NextAttemptAt.After(now)) {
			return false
		}
		limited, _ := r.finalize(ctx, p, rec, start, end, loc, now) // stored on the row and logged
		return limited
	case domain.TopPlaylistEmpty:
		return false
	case domain.TopPlaylistFailed, domain.TopPlaylistWaitingAuth:
		if rec.NextAttemptAt != nil && rec.NextAttemptAt.After(now) {
			return false
		}
	}
	limited, _ := r.attempt(ctx, p, rec, start, end, loc, now, false) // outcome stored on the row and logged
	return limited
}

// attempt ranks the period [start, end) (local midnights in loc), stores the
// snapshot and publishes it: it creates the Spotify playlist when the row
// has none, otherwise it replaces the items of the same playlist. Every
// outcome is stored on the row; the error is also returned (errors.Is
// ErrNotAuthorized for waiting_auth) and limited reports a rate limit.
//
// With live set the period is still running: it ranks [start, now), writes
// nothing when that ranks no tracks (ErrNoPlays) and publishes a live row
// with the live description.
func (r *Runner) attempt(ctx context.Context, p Period, rec domain.TopPlaylistRecord, start, end time.Time,
	loc *time.Location, now time.Time, live bool) (limited bool, err error) {
	rec.PeriodStart, rec.PeriodEnd, rec.TimeZone = start, end, loc.String()
	rec.Name = Name(p, start)
	prev := rec.Status
	// A playlist created by a live attempt still has the live description.
	staleDetails := !live && rec.SpotifyPlaylistID != "" && rec.RefreshedAt != nil

	var tracks []domain.RankedTrack
	if live {
		// Rank first: a period without plays yet gets no row at all.
		if tracks, err = r.Store.RankTracks(ctx, start, now, TrackLimit); err != nil {
			r.Log.WarnContext(ctx, "rank live top playlist failed", slog.String("name", rec.Name), logging.Err(err))
			return false, err
		}
		if len(tracks) == 0 {
			return false, ErrNoPlays
		}
	}

	msg, err := r.authProblem(ctx)
	if err != nil {
		r.Log.WarnContext(ctx, "read spotify authorization failed", logging.Err(err))
		return false, err
	}
	if msg != "" {
		r.waitAuth(ctx, rec, prev, msg, nil)
		return false, fmt.Errorf("%w: %s", ErrNotAuthorized, msg)
	}

	if !live {
		if tracks, err = r.Store.RankTracks(ctx, start, end, TrackLimit); err != nil {
			return r.fail(ctx, rec, err, now), err
		}
	}
	if len(tracks) == 0 {
		rec.Status, rec.TrackCount, rec.TotalPlays = domain.TopPlaylistEmpty, 0, 0
		rec.LastError, rec.NextAttemptAt = "", nil
		wctx, cancel := storeCtx(ctx)
		defer cancel()
		if err := r.Store.UpsertTopPlaylist(wctx, rec); err != nil {
			r.Log.WarnContext(ctx, "save empty top playlist failed", slog.String("name", rec.Name), logging.Err(err))
			return false, err
		}
		r.Log.InfoContext(ctx, "top playlist skipped: no plays in period", slog.String("name", rec.Name))
		return false, nil
	}

	rec.Status = domain.TopPlaylistPending
	rec.TrackCount, rec.TotalPlays = len(tracks), 0
	for _, t := range tracks {
		rec.TotalPlays += t.Plays
	}
	if err := r.saveSnapshot(ctx, rec, tracks); err != nil {
		return r.fail(ctx, rec, err, now), err
	}

	if rec.SpotifyPlaylistID == "" {
		var err error
		descr := Description(p, start)
		if live {
			descr = LiveDescription(p, start)
			refreshed := now.UTC()
			rec.RefreshedAt = &refreshed // marks the playlist as created live
		}
		if rec, err = r.create(ctx, rec, descr); err != nil {
			if rec.SpotifyPlaylistID != "" {
				return false, err // created but not saved: logged; retried next tick
			}
			return r.spotifyError(ctx, rec, prev, err, now)
		}
	}

	uris := make([]string, len(tracks))
	for i, t := range tracks {
		uris[i] = "spotify:track:" + t.TrackID
	}
	if err := r.Spotify.ReplacePlaylistItems(ctx, rec.SpotifyPlaylistID, uris); err != nil {
		return r.spotifyError(ctx, rec, prev, err, now)
	}
	if staleDetails {
		if err := r.Spotify.UpdatePlaylistDetails(ctx, rec.SpotifyPlaylistID, rec.Name, Description(p, start)); err != nil {
			return r.spotifyError(ctx, rec, prev, err, now)
		}
	}

	published := now.UTC()
	rec.Status, rec.PublishedAt, rec.Attempts = domain.TopPlaylistCreated, &published, 0
	rec.LastError, rec.NextAttemptAt = "", nil
	rec.Live = live
	if live || staleDetails {
		rec.RefreshedAt = &published
	}
	if err := r.upsert(ctx, rec); err != nil {
		return r.fail(ctx, rec, err, now), err
	}
	r.Log.InfoContext(ctx, "top playlist published", slog.String("name", rec.Name),
		slog.String("spotify_id", rec.SpotifyPlaylistID), slog.Int("tracks", rec.TrackCount))
	if r.Invalidate != nil {
		r.Invalidate(ctx)
	}
	return false, nil
}

// create makes the Spotify playlist and persists its ID before anything
// else can fail, so a retry reuses it. Both steps run detached from shutdown
// (bounded by createTimeout): a create that reached Spotify is always saved.
// On a save error the returned record carries the new ID and err is set.
func (r *Runner) create(ctx context.Context, rec domain.TopPlaylistRecord, description string) (domain.TopPlaylistRecord, error) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), createTimeout)
	defer cancel()
	id, link, err := r.Spotify.CreatePlaylist(cctx, rec.Name, description, true)
	if err != nil {
		return rec, err
	}
	rec.SpotifyPlaylistID, rec.SpotifyURL = id, link
	if err := r.Store.UpsertTopPlaylist(cctx, rec); err != nil {
		r.Log.ErrorContext(ctx, "save created spotify playlist id failed",
			slog.String("name", rec.Name), slog.String("spotify_id", id), logging.Err(err))
		return rec, err
	}
	return rec, nil
}

// authProblem returns a waiting_auth message when Spotify is not connected
// or the stored scopes lack playlist-modify-public. Unknown scopes (a token
// seeded from the environment, never refreshed) are given a try: a 403
// leads to waiting_auth as well.
func (r *Runner) authProblem(ctx context.Context) (string, error) {
	s, err := r.Auth.Spotify(ctx)
	switch {
	case errors.Is(err, settings.ErrNotFound):
		return msgNotConnected, nil
	case err != nil:
		return "", err
	case s.RefreshToken == "":
		return msgNotConnected, nil
	case len(s.Scopes) > 0 && !spotify.HasScope(s.Scopes, spotify.PlaylistWriteScope):
		return msgMissingScope, nil
	}
	return "", nil
}

// spotifyError records a failed Spotify call: missing permissions wait for
// re-authorization (the returned error wraps ErrNotAuthorized and the cause),
// everything else is retried with backoff.
func (r *Runner) spotifyError(ctx context.Context, rec domain.TopPlaylistRecord, prev string, err error, now time.Time) (bool, error) {
	msg, backoff, ok := authMessage(err)
	if !ok {
		return r.fail(ctx, rec, err, now), err
	}
	var retryAt *time.Time
	if backoff {
		retry := now.Add(forbiddenRetry).UTC()
		retryAt = &retry
	}
	r.waitAuth(ctx, rec, prev, msg, retryAt)
	return false, fmt.Errorf("%w: %s: %w", ErrNotAuthorized, msg, err)
}

// authMessage maps a Spotify authorization error to the waiting_auth message.
// backoff is set after a 403, whose retry is spaced out by forbiddenRetry.
func authMessage(err error) (msg string, backoff, ok bool) {
	switch {
	case errors.Is(err, spotify.ErrForbidden):
		return msgMissingScope, true, true
	case errors.Is(err, spotify.ErrMissingScope):
		return msgMissingScope, false, true
	case errors.Is(err, spotify.ErrInvalidGrant):
		return msgRevoked, false, true
	case errors.Is(err, spotify.ErrNotConnected):
		return msgNotConnected, false, true
	}
	return "", false, false
}

// waitAuth stores status waiting_auth, logging only when the status changes.
// retryAt, when set, holds off the next attempt (used after a 403).
func (r *Runner) waitAuth(ctx context.Context, rec domain.TopPlaylistRecord, prev, msg string, retryAt *time.Time) {
	if prev == domain.TopPlaylistWaitingAuth && rec.LastError == msg && retryAt == nil && rec.NextAttemptAt == nil {
		return // unchanged; nothing to write or log
	}
	rec.Status, rec.LastError, rec.NextAttemptAt = domain.TopPlaylistWaitingAuth, msg, retryAt
	if err := r.upsert(ctx, rec); err != nil {
		r.Log.WarnContext(ctx, "save top playlist state failed", slog.String("name", rec.Name), logging.Err(err))
		return
	}
	if prev != domain.TopPlaylistWaitingAuth {
		r.Log.WarnContext(ctx, "top playlist waiting for spotify authorization",
			slog.String("name", rec.Name), slog.String("reason", msg))
	}
}

// fail records a failed attempt and schedules a retry. It reports whether
// the failure was a Spotify rate limit.
func (r *Runner) fail(ctx context.Context, rec domain.TopPlaylistRecord, cause error, now time.Time) bool {
	rec.Attempts++
	delay := Backoff(rec.Attempts)
	var rl *spotify.RateLimitError
	limited := errors.As(cause, &rl)
	if limited && rl.RetryAfter > delay {
		delay = rl.RetryAfter
	}
	next := now.Add(delay).UTC()
	rec.Status, rec.NextAttemptAt = domain.TopPlaylistFailed, &next
	rec.LastError = truncateError(cause.Error())
	if err := r.upsert(ctx, rec); err != nil {
		r.Log.WarnContext(ctx, "save top playlist failure failed", slog.String("name", rec.Name), logging.Err(err))
	}
	r.Log.WarnContext(ctx, "top playlist generation failed", slog.String("name", rec.Name),
		slog.Int("attempts", rec.Attempts), slog.Time("next_attempt_at", next), logging.Err(cause))
	return limited
}

// Backoff is the retry delay after n failed attempts: 5 min doubling up to 6 h.
func Backoff(n int) time.Duration {
	d := baseBackoff
	for i := 1; i < n && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

func truncateError(s string) string {
	r := []rune(s)
	if len(r) > maxErrorLen {
		return string(r[:maxErrorLen-1]) + "…"
	}
	return s
}

func (r *Runner) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *Runner) wait(ctx context.Context, d time.Duration) bool {
	if r.sleep != nil {
		return r.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
