// Package domain holds the core types shared by the store, the gRPC and the
// HTTP layers.
package domain

import (
	"errors"
	"strings"
	"time"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// Artist is a Spotify artist.
type Artist struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URI         string `json:"uri,omitempty"`
	ExternalURL string `json:"external_url,omitempty"`
}

// Album is a Spotify album.
type Album struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ReleaseDate   string `json:"release_date,omitempty"`
	CoverImageURL string `json:"cover_image_url,omitempty"`
	URI           string `json:"uri,omitempty"`
	ExternalURL   string `json:"external_url,omitempty"`
}

// Track is a Spotify track.
type Track struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URI         string `json:"uri,omitempty"`
	ISRC        string `json:"isrc,omitempty"`
	DurationMS  int32  `json:"duration_ms"`
	Explicit    bool   `json:"explicit"`
	ExternalURL string `json:"external_url,omitempty"`
	AlbumID     string `json:"-"`
}

// TrackView is a track hydrated with its album and artists.
type TrackView struct {
	Track
	Album   *Album   `json:"album,omitempty"`
	Artists []Artist `json:"artists"`
}

// Playback is one "currently playing" snapshot from Spotify.
type Playback struct {
	Track      Track
	Album      Album
	Artists    []Artist
	IsPlaying  bool
	ProgressMS *int32
	PlayedAt   time.Time
	FetchedAt  time.Time
	Context    *PlaybackContext
}

// PlaybackContext is what a track is played from.
type PlaybackContext struct {
	Type        string `json:"type"` // playlist | album | artist | show | collection
	URI         string `json:"uri"`
	ExternalURL string `json:"external_url,omitempty"`
}

// ID returns the Spotify ID from the context URI (spotify:<type>:<id>).
func (c PlaybackContext) ID() string {
	parts := strings.Split(c.URI, ":")
	if len(parts) >= 3 && parts[0] == "spotify" {
		return parts[len(parts)-1]
	}
	return ""
}

// Playlist is Spotify playlist metadata.
type Playlist struct {
	ID            string    `json:"id"`
	Available     bool      `json:"available"`
	Name          string    `json:"name,omitempty"`
	Description   string    `json:"description,omitempty"`
	OwnerID       string    `json:"owner_id,omitempty"`
	OwnerName     string    `json:"owner_name,omitempty"`
	CoverImageURL string    `json:"cover_image_url,omitempty"`
	ExternalURL   string    `json:"external_url,omitempty"`
	URI           string    `json:"uri,omitempty"`
	Public        *bool     `json:"public"`
	Collaborative bool      `json:"collaborative"`
	TracksTotal   *int32    `json:"tracks_total"`
	SnapshotID    string    `json:"-"`
	FetchedAt     time.Time `json:"fetched_at"`
}

// RecentPlaylist is a playlist ranked by when it was last played from.
type RecentPlaylist struct {
	Playlist   Playlist  `json:"playlist"`
	Plays      int64     `json:"plays"`
	LastPlayed time.Time `json:"last_played"`
}

// NowPlaying is the public view of the current playback state.
type NowPlaying struct {
	IsPlaying  bool       `json:"is_playing"`
	ProgressMS *int32     `json:"progress_ms"`
	FetchedAt  time.Time  `json:"fetched_at"`
	Track      *TrackView `json:"track"`
}

// HistoryEntry is one listening history row.
type HistoryEntry struct {
	ID          string    `json:"id"`
	PlayedAt    time.Time `json:"played_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ReplayCount int32     `json:"replay_count"`
	Track       TrackView `json:"track"`
}

// TopTrack is a track ranked by plays.
type TopTrack struct {
	Track      TrackView `json:"track"`
	Plays      int64     `json:"plays"`
	LastPlayed time.Time `json:"last_played"`
}

// TopArtist is an artist ranked by plays.
type TopArtist struct {
	Artist     Artist    `json:"artist"`
	Plays      int64     `json:"plays"`
	LastPlayed time.Time `json:"last_played"`
}

// GenreStat aggregates plays per root genre.
type GenreStat struct {
	Genre string         `json:"genre"`
	Plays int64          `json:"plays"`
	Share float64        `json:"share"`
	Sub   []SubGenreStat `json:"sub"`
}

// SubGenreStat aggregates plays per sub-genre.
type SubGenreStat struct {
	Genre string `json:"genre"`
	Plays int64  `json:"plays"`
}

// AudioProfile is the play-weighted average of audio features.
type AudioProfile struct {
	SampleTracks     int64    `json:"sample_tracks"`
	SamplePlays      int64    `json:"sample_plays"`
	Acousticness     *float64 `json:"acousticness"`
	Danceability     *float64 `json:"danceability"`
	Energy           *float64 `json:"energy"`
	Instrumentalness *float64 `json:"instrumentalness"`
	Liveness         *float64 `json:"liveness"`
	Loudness         *float64 `json:"loudness"`
	Speechiness      *float64 `json:"speechiness"`
	Tempo            *float64 `json:"tempo"`
	Valence          *float64 `json:"valence"`
}

// Summary is a set of overall listening counters.
type Summary struct {
	TotalPlays     int64      `json:"total_plays"`
	UniqueTracks   int64      `json:"unique_tracks"`
	UniqueArtists  int64      `json:"unique_artists"`
	ListeningMS    int64      `json:"listening_ms"`
	FirstPlayedAt  *time.Time `json:"first_played_at"`
	LastPlayedAt   *time.Time `json:"last_played_at"`
	EnrichedTracks int64      `json:"enriched_tracks"`
}

// Genre is a provider genre with sub-genres.
type Genre struct {
	Root string   `json:"root"`
	Sub  []string `json:"sub"`
}

// AudioFeatures holds per-track audio features; nil means unknown.
type AudioFeatures struct {
	Acousticness     *float64 `json:"acousticness"`
	Danceability     *float64 `json:"danceability"`
	Energy           *float64 `json:"energy"`
	Instrumentalness *float64 `json:"instrumentalness"`
	Key              *int32   `json:"key"`
	Liveness         *float64 `json:"liveness"`
	Loudness         *float64 `json:"loudness"`
	Mode             *int32   `json:"mode"`
	Speechiness      *float64 `json:"speechiness"`
	Tempo            *float64 `json:"tempo"`
	TimeSignature    *int32   `json:"time_signature"`
	Valence          *float64 `json:"valence"`
}

// Enrichment statuses.
const (
	EnrichmentPending     = "pending"
	EnrichmentEnriched    = "enriched"
	EnrichmentUnavailable = "unavailable"
	EnrichmentFailed      = "failed"
)

// TrackFeatures is the enrichment record of a track.
type TrackFeatures struct {
	TrackID    string        `json:"-"`
	Status     string        `json:"status"`
	Source     string        `json:"source,omitempty"`
	Features   AudioFeatures `json:"features"`
	Genres     []Genre       `json:"genres"`
	EnrichedAt *time.Time    `json:"enriched_at"`
}

// TrackDetail is a track with its features and play counters.
type TrackDetail struct {
	TrackView
	Features   *TrackFeatures `json:"features"`
	Plays      int64          `json:"plays"`
	LastPlayed *time.Time     `json:"last_played"`
}

// PendingTrack is a track awaiting enrichment.
type PendingTrack struct {
	TrackID  string
	ISRC     string
	Attempts int32
}

// Range is a look-back window for statistics. A zero Since means all time.
type Range struct {
	Name  string
	Since time.Time
}

// ParseRange maps a query value ("7d", "30d", "90d", "365d", "all") to a Range.
func ParseRange(s string, now time.Time) (Range, bool) {
	days := map[string]int{"7d": 7, "30d": 30, "90d": 90, "180d": 180, "365d": 365}
	switch s {
	case "", "30d":
		return Range{Name: "30d", Since: now.AddDate(0, 0, -30)}, true
	case "all":
		return Range{Name: "all"}, true
	}
	if d, ok := days[s]; ok {
		return Range{Name: s, Since: now.AddDate(0, 0, -d)}, true
	}
	return Range{}, false
}
