package domain

// Lyrics statuses. LyricsPending is API-only: the track has no lyrics row yet.
const (
	LyricsFound        = "found"
	LyricsInstrumental = "instrumental"
	LyricsNotFound     = "not_found"
	LyricsError        = "error"
	LyricsPending      = "pending"
)

// LyricsCandidate is a track whose lyrics should be looked up.
type LyricsCandidate struct {
	TrackID    string
	Name       string
	Artist     string // first credited artist
	Album      string
	DurationMS int32
	Status     string // current lyrics status: not_found, error, or "" (never looked up)
	Attempts   int32  // failed lookups with this status so far
}

// Lyrics is a successful lookup (Status LyricsFound or LyricsInstrumental).
// Lyrics are admin and MCP only: no public view embeds this type.
type Lyrics struct {
	Status   string
	LRCLibID int64
	Plain    string
	Synced   string // LRC text, "[mm:ss.xx] line" per line
}
