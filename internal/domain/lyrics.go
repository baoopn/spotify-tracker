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

// Translation statuses (lyrics_translations.status). TranslationPending is
// API-only: there is no current translation yet.
const (
	TranslationDone    = "done"
	TranslationSkipped = "skipped" // nothing to translate, or too long
	TranslationRefused = "refused" // the model declined (terminal)
	TranslationError   = "error"   // retried at next_attempt_at; NULL = given up
	TranslationPending = "pending"
	// TranslationNotQueued is admin-only: the track was never queued (songs
	// are queued when they play; there is no backfill).
	TranslationNotQueued = "not_queued"
)

// Translation kinds: synced lyrics are translated line by line, plain lyrics
// as one text.
const (
	TranslationSynced = "synced"
	TranslationPlain  = "plain"
)

// TranslationCandidate is found lyrics whose translation is missing, stale
// (the lyrics changed), or due for a retry.
type TranslationCandidate struct {
	TrackID    string
	Name       string
	Artist     string // first credited artist
	Synced     string // LRC text, "" when none
	Plain      string
	SourceHash string // hash of the lyrics as read; the translation is stored only if it still matches
	Status     string // current translation status, "" when there is none
	Attempts   int32
}

// Translation is a finished translation. For synced lyrics EN and VI hold
// one entry per parsed LRC line; for plain lyrics ENText and VIText hold the
// text. A nil / empty language means the song is already in that language.
type Translation struct {
	Kind           string
	SourceLanguage string
	EN, VI         []string
	ENText, VIText string
	Model          string
	InputTokens    int64
	OutputTokens   int64
}
