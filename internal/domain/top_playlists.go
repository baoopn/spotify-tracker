package domain

import "time"

// Top playlist statuses (top_playlists.status).
const (
	TopPlaylistPending     = "pending"
	TopPlaylistCreated     = "created"
	TopPlaylistEmpty       = "empty"
	TopPlaylistWaitingAuth = "waiting_auth"
	TopPlaylistFailed      = "failed"
)

// TopPlaylistRecord is one auto-generated "Top 50" playlist of a period
// (week, month or year) and its generation state. A live record is the
// current, unfinished period's playlist, refreshed while listening and
// finalized in place when the period ends.
type TopPlaylistRecord struct {
	Period            string
	PeriodStart       time.Time
	PeriodEnd         time.Time
	TimeZone          string
	Name              string
	Status            string
	SpotifyPlaylistID string
	SpotifyURL        string
	TrackCount        int
	TotalPlays        int64
	Attempts          int
	LastError         string
	NextAttemptAt     *time.Time
	PublishedAt       *time.Time
	// Hidden rows are left out of the public API (toggled by admins).
	Hidden bool
	// Live is set while the playlist tracks an unfinished period.
	Live bool
	// RefreshedAt is the last live refresh (or the finalization).
	RefreshedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RankedTrack is a track's play count within a period.
type RankedTrack struct {
	TrackID string
	Plays   int64
}

// TopPlaylistTrack is one position of a top playlist snapshot.
type TopPlaylistTrack struct {
	Position int       `json:"position"`
	Plays    int64     `json:"plays"`
	Track    TrackView `json:"track"`
}
