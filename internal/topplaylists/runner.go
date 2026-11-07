package topplaylists

import (
	"context"
	"errors"
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
)

const (
	maxErrorLen = 300
	baseBackoff = 5 * time.Minute
	maxBackoff  = 6 * time.Hour
)

// Store is the persistence the runner needs (implemented by *store.Store).
type Store interface {
	TopPlaylist(ctx context.Context, period string, start time.Time) (domain.TopPlaylistRecord, error)
	UpsertTopPlaylist(ctx context.Context, p domain.TopPlaylistRecord) error
	SaveTopPlaylistSnapshot(ctx context.Context, p domain.TopPlaylistRecord, tracks []domain.RankedTrack) error
	RankTracks(ctx context.Context, start, end time.Time, limit int) ([]domain.RankedTrack, error)
}

// Spotify writes playlists (implemented by *spotify.Manager).
type Spotify interface {
	CreatePlaylist(ctx context.Context, name, description string, public bool) (id, url string, err error)
	ReplacePlaylistItems(ctx context.Context, id string, uris []string) error
}

// Auth reads the stored Spotify authorization (implemented by *settings.Store).
type Auth interface {
	Spotify(ctx context.Context) (settings.Spotify, error)
}

// Runner creates the top playlist of the most recently completed period of
// every enabled kind. Each period's row in top_playlists is its state, so the
// runner is safe to restart at any point and never creates duplicates.
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
		FirstDelay: time.Minute, Interval: 10 * time.Minute}
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

// RunOnce handles the last completed period of every enabled kind.
func (r *Runner) RunOnce(ctx context.Context) {
	now := r.clock()
	for _, p := range r.Config.Periods {
		if rateLimited := r.generate(ctx, p, now); rateLimited {
			return // the other kinds would be refused too; try next tick
		}
	}
}

// generate advances one period's state machine. It reports whether Spotify
// rate-limited the request.
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
	case domain.TopPlaylistCreated, domain.TopPlaylistEmpty:
		return false
	case domain.TopPlaylistFailed:
		if rec.NextAttemptAt != nil && rec.NextAttemptAt.After(now) {
			return false
		}
	}
	rec.PeriodStart, rec.PeriodEnd, rec.TimeZone = start, end, loc.String()
	rec.Name = Name(p, start)
	prev := rec.Status

	msg, err := r.authProblem(ctx)
	if err != nil {
		r.Log.WarnContext(ctx, "read spotify authorization failed", logging.Err(err))
		return false
	}
	if msg != "" {
		r.waitAuth(ctx, rec, prev, msg)
		return false
	}

	tracks, err := r.Store.RankTracks(ctx, start, end, TrackLimit)
	if err != nil {
		return r.fail(ctx, rec, err, now)
	}
	if len(tracks) == 0 {
		rec.Status, rec.TrackCount, rec.TotalPlays = domain.TopPlaylistEmpty, 0, 0
		rec.LastError, rec.NextAttemptAt = "", nil
		if err := r.Store.UpsertTopPlaylist(ctx, rec); err != nil {
			r.Log.WarnContext(ctx, "save empty top playlist failed", slog.String("name", rec.Name), logging.Err(err))
			return false
		}
		r.Log.InfoContext(ctx, "top playlist skipped: no plays in period", slog.String("name", rec.Name))
		return false
	}

	rec.Status = domain.TopPlaylistPending
	rec.TrackCount, rec.TotalPlays = len(tracks), 0
	for _, t := range tracks {
		rec.TotalPlays += t.Plays
	}
	if err := r.Store.SaveTopPlaylistSnapshot(ctx, rec, tracks); err != nil {
		return r.fail(ctx, rec, err, now)
	}

	if rec.SpotifyPlaylistID == "" {
		id, link, err := r.Spotify.CreatePlaylist(ctx, rec.Name, Description(p, start), true)
		if err != nil {
			return r.spotifyError(ctx, rec, prev, err, now)
		}
		rec.SpotifyPlaylistID, rec.SpotifyURL = id, link
		// Persist the ID before anything else can fail, so a retry reuses it.
		if err := r.Store.UpsertTopPlaylist(ctx, rec); err != nil {
			r.Log.ErrorContext(ctx, "save created spotify playlist id failed",
				slog.String("name", rec.Name), slog.String("spotify_id", id), logging.Err(err))
			return false
		}
	}

	uris := make([]string, len(tracks))
	for i, t := range tracks {
		uris[i] = "spotify:track:" + t.TrackID
	}
	if err := r.Spotify.ReplacePlaylistItems(ctx, rec.SpotifyPlaylistID, uris); err != nil {
		return r.spotifyError(ctx, rec, prev, err, now)
	}

	published := now.UTC()
	rec.Status, rec.PublishedAt, rec.Attempts = domain.TopPlaylistCreated, &published, 0
	rec.LastError, rec.NextAttemptAt = "", nil
	if err := r.Store.UpsertTopPlaylist(ctx, rec); err != nil {
		return r.fail(ctx, rec, err, now)
	}
	r.Log.InfoContext(ctx, "top playlist published", slog.String("name", rec.Name),
		slog.String("spotify_id", rec.SpotifyPlaylistID), slog.Int("tracks", rec.TrackCount))
	if r.Invalidate != nil {
		r.Invalidate(ctx)
	}
	return false
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
// re-authorization, everything else is retried with backoff.
func (r *Runner) spotifyError(ctx context.Context, rec domain.TopPlaylistRecord, prev string, err error, now time.Time) bool {
	switch {
	case errors.Is(err, spotify.ErrForbidden), errors.Is(err, spotify.ErrMissingScope), errors.Is(err, spotify.ErrInvalidGrant):
		r.waitAuth(ctx, rec, prev, msgMissingScope)
		return false
	case errors.Is(err, spotify.ErrNotConnected):
		r.waitAuth(ctx, rec, prev, msgNotConnected)
		return false
	}
	return r.fail(ctx, rec, err, now)
}

// waitAuth stores status waiting_auth, logging only when the status changes.
func (r *Runner) waitAuth(ctx context.Context, rec domain.TopPlaylistRecord, prev, msg string) {
	if prev == domain.TopPlaylistWaitingAuth && rec.LastError == msg {
		return // unchanged; nothing to write or log
	}
	rec.Status, rec.LastError, rec.NextAttemptAt = domain.TopPlaylistWaitingAuth, msg, nil
	if err := r.Store.UpsertTopPlaylist(ctx, rec); err != nil {
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
	if err := r.Store.UpsertTopPlaylist(ctx, rec); err != nil {
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
