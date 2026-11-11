package topplaylists

import (
	"context"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/settings"
)

// Store is the persistence the runner needs (implemented by *store.Store).
type Store interface {
	TopPlaylist(ctx context.Context, period string, start time.Time) (domain.TopPlaylistRecord, error)
	// UpsertTopPlaylist never changes a pruned row.
	UpsertTopPlaylist(ctx context.Context, p domain.TopPlaylistRecord) error
	SaveTopPlaylistSnapshot(ctx context.Context, p domain.TopPlaylistRecord, tracks []domain.RankedTrack) error
	// TopPlaylistSnapshot returns the stored ranking in position order.
	TopPlaylistSnapshot(ctx context.Context, period string, start time.Time) ([]domain.RankedTrack, error)
	// EndedLiveTopPlaylists lists created live rows whose period ended at or
	// before endedBy and that are not backing off at now, oldest first.
	EndedLiveTopPlaylists(ctx context.Context, endedBy, now time.Time, limit int) ([]domain.TopPlaylistRecord, error)
	RankTracks(ctx context.Context, start, end time.Time, limit int) ([]domain.RankedTrack, error)
	// LockTopPlaylists takes a cluster-wide lock so overlapping instances
	// (e.g. during a rolling deploy) never generate at the same time.
	LockTopPlaylists(ctx context.Context) (unlock func(), ok bool, err error)

	// CreatedTopPlaylists lists the created rows of one kind (live or not,
	// hidden or not), newest period first.
	CreatedTopPlaylists(ctx context.Context, period string) ([]domain.TopPlaylistRecord, error)
	// StuckTopPlaylists lists the failed and waiting_auth rows of one kind
	// that hold a Spotify playlist, newest period first.
	StuckTopPlaylists(ctx context.Context, period string) ([]domain.TopPlaylistRecord, error)
	// AllCreatedTopPlaylists lists the created rows of every kind, oldest first.
	AllCreatedTopPlaylists(ctx context.Context) ([]domain.TopPlaylistRecord, error)
	// PruneTopPlaylist marks a row pruned (not live, removal pending) and
	// deletes its snapshot.
	PruneTopPlaylist(ctx context.Context, period string, start, now time.Time) error
	// PendingUnfollows lists pruned rows whose Spotify playlist is not
	// confirmed removed yet, oldest first.
	PendingUnfollows(ctx context.Context, limit int) ([]domain.TopPlaylistRecord, error)
	MarkSpotifyRemoved(ctx context.Context, period string, start time.Time) error
	// SetTopPlaylistError stores a row's last error and nothing else.
	SetTopPlaylistError(ctx context.Context, period string, start time.Time, msg string) error
	MarkVerified(ctx context.Context, keys []domain.TopPlaylistKey, now time.Time) error
	// LastTopPlaylistVerify is the newest verified_at among created rows.
	LastTopPlaylistVerify(ctx context.Context) (*time.Time, error)
}

// Spotify writes playlists (implemented by *spotify.Manager).
type Spotify interface {
	CreatePlaylist(ctx context.Context, name, description string, public bool) (id, url string, err error)
	ReplacePlaylistItems(ctx context.Context, id string, uris []string) error
	UpdatePlaylistDetails(ctx context.Context, id, name, description string) error
	// UnfollowPlaylist deletes an owned playlist; already gone is not an error.
	UnfollowPlaylist(ctx context.Context, id string) error
	// FollowPlaylist restores a deleted playlist; spotify.ErrNotFound when
	// it no longer exists.
	FollowPlaylist(ctx context.Context, id string) error
	// OwnedPlaylistIDs lists the playlists in the user's library.
	OwnedPlaylistIDs(ctx context.Context) (map[string]bool, error)
	// FindOwnedPlaylist returns an owned playlist named exactly name
	// ("" when there is none).
	FindOwnedPlaylist(ctx context.Context, name string) (id, url string, err error)
	// CurrentUserName is the connected account's display name (or ID).
	CurrentUserName(ctx context.Context) (string, error)
}

// Auth reads the stored Spotify authorization (implemented by *settings.Store).
type Auth interface {
	Spotify(ctx context.Context) (settings.Spotify, error)
}
