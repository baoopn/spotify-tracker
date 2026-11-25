package mcpserver

// param is one tool argument. It is passed to the API as a query parameter,
// or as a path segment when inPath is set (the tool path then holds
// "{name}").
type param struct {
	name, desc string
	integer    bool
	boolean    bool // a JSON boolean
	enum       []string
	min, max   int // integer bounds (used when integer is set)
	maxLen     int // string length bound (0 = none)
	def        any // default value, also the API's default (nil = none)
	required   bool
	inPath     bool
	inBody     bool // sent in the JSON request body (write tools), not the query string
}

// toolDef maps an MCP tool to one route of an in-process handler: a GET
// (read-only) unless method says otherwise.
type toolDef struct {
	name, title, desc string
	path              string
	params            []param
	method            string // "" = GET
	destructive       bool   // the write removes data (MCP destructiveHint)
}

var (
	statsRanges     = []string{"7d", "30d", "90d", "180d", "365d", "all"}
	analyticsRanges = []string{"24h", "7d", "30d"}
	topPeriods      = []string{"week", "month", "year"}
	sortOrders      = []string{"asc", "desc"}
)

func rangeParam() param {
	return param{name: "range", enum: statsRanges, def: "30d",
		desc: "Time window: the last 7, 30, 90, 180 or 365 days, or all time."}
}

func limitParam(def, hi int) param {
	return param{name: "limit", integer: true, min: 1, max: hi, def: def, desc: "Number of items to return."}
}

func offsetParam() param {
	return param{name: "offset", integer: true, min: 0, max: 1_000_000, def: 0, desc: "Number of items to skip (paging)."}
}

func searchParam(desc string) param {
	return param{name: "q", maxLen: 200, desc: desc}
}

func cursorParam() param {
	return param{name: "cursor", maxLen: 64,
		desc: "Opaque paging cursor: the next_cursor value of the previous page. Omit for the newest entries."}
}

func idParam(desc string) param {
	return param{name: "id", maxLen: 64, required: true, inPath: true, desc: desc}
}

// publicTools are served to every caller (paths relative to /api/v1).
var publicTools = []toolDef{
	{name: "now_playing", title: "Now playing", path: "/now-playing",
		desc: "The track playing on Spotify right now, with progress; data is null when nothing has played yet."},
	{name: "recent_plays", title: "Recent plays", path: "/history",
		desc:   "Recently played tracks, newest first. Use next_cursor from the result as cursor to get older plays.",
		params: []param{limitParam(20, 100), cursorParam()}},
	{name: "track_details", title: "Track details", path: "/tracks/{id}",
		desc: "Details of one track: track and disc number, album (type, size, release date precision, " +
			"UPC, copyrights), artists with Spotify genres, play stats and audio features (when enriched).",
		params: []param{idParam("Spotify track ID (the 22-character base-62 ID, not a URI or URL).")}},
	{name: "listening_summary", title: "Listening summary", path: "/stats/summary",
		desc:   "Totals for a time window: plays, listening time, unique tracks and artists, first and last play.",
		params: []param{rangeParam()}},
	{name: "top_tracks", title: "Top tracks", path: "/stats/top-tracks",
		desc: "Most played tracks in a time window.", params: []param{rangeParam(), limitParam(20, 100)}},
	{name: "top_artists", title: "Top artists", path: "/stats/top-artists",
		desc: "Most played artists in a time window.", params: []param{rangeParam(), limitParam(20, 100)}},
	{name: "top_genres", title: "Top genres", path: "/stats/genres",
		desc: "Most played genres in a time window.", params: []param{rangeParam(), limitParam(20, 100)}},
	{name: "audio_profile", title: "Audio profile", path: "/stats/audio-profile",
		desc:   "Average audio features (energy, danceability, valence, tempo, ...) of the tracks played in a time window.",
		params: []param{rangeParam()}},
	{name: "recent_playlists", title: "Recent playlists", path: "/playlists/recent",
		desc: "Playlists recently played from.", params: []param{limitParam(5, 50)}},
	{name: "top_playlists", title: "Top 50 playlists", path: "/playlists/top",
		desc: "The auto-generated \"Top 50\" Spotify playlists per period (week, month, year). " +
			"The newest one of each period includes its tracks.",
		params: []param{{name: "limit", integer: true, min: 1, max: 24, def: 6,
			desc: "Number of older playlists to list per period, besides the newest one."}}},
}

// adminTools are served only to callers with the admin token (paths
// relative to /admin/api). All are read-only.
var adminTools = []toolDef{
	{name: "admin_overview", title: "Admin overview", path: "/overview",
		desc: "Service health (api, poller, enricher, postgres, redis), now playing, all-time and 7-day summaries, " +
			"enrichment queue counts, Spotify connection status, and up_next: the next tracks of the Spotify queue (private, null when none)."},
	{name: "admin_spotify_status", title: "Spotify connection", path: "/spotify",
		desc: "Spotify connection status: connected account, scopes, authorization date, refresh-token expiry, last error. " +
			"Contains no tokens."},
	{name: "admin_history", title: "Play history (admin)", path: "/history",
		desc:   "Full play history, newest first, with search. Use next_cursor from the result as cursor for older plays.",
		params: []param{limitParam(50, 100), cursorParam(), searchParam("Search by track, artist or album name, or a Spotify ID/URI.")}},
	{name: "admin_tracks", title: "Tracks (admin)", path: "/tracks",
		desc: "Every tracked track with play counts and enrichment status. Result has data and total.",
		params: []param{
			searchParam("Search by track, artist or album name, ISRC, or a Spotify ID/URI."),
			{name: "status", enum: []string{"pending", "enriched", "unavailable", "failed"},
				desc: "Only tracks with this enrichment status."},
			{name: "sort", enum: []string{"plays", "last_played", "name", "created"}, def: "last_played", desc: "Sort column."},
			{name: "order", enum: sortOrders, def: "desc", desc: "Sort direction."},
			limitParam(25, 100), offsetParam(),
		}},
	{name: "admin_track", title: "Track (admin)", path: "/tracks/{id}",
		desc: "One track with its track and disc number, album (type, size, release date precision, UPC, " +
			"copyrights), artists with Spotify genres, audio features, enrichment status and play stats.",
		params: []param{idParam("Spotify track ID.")}},
	{name: "admin_track_lyrics", title: "Track lyrics (admin)", path: "/tracks/{id}/lyrics",
		desc: "Lyrics of one track (from LRCLIB): status (found, instrumental, not_found, error, or pending = " +
			"not looked up yet), queued / looking_up (the lookup queue), next_attempt_at, plain lyrics, " +
			"has_synced, and with synced=true the time-synced LRC text. " +
			"translation: status (pending, done, skipped, refused, error), stale, source_language (main), languages " +
			"(every language in the lyrics, main first), and en / vi machine translations by Claude (null only when " +
			"the song is all in that language; for synced lyrics " +
			"one line per LRC line, joined with newlines).",
		params: []param{idParam("Spotify track ID."),
			{name: "synced", enum: []string{"true", "false"}, def: "false",
				desc: "Also return the time-synced LRC lyrics."}}},
	{name: "admin_search_lyrics", title: "Search lyrics (admin)", path: "/lyrics/search",
		desc: "Find tracks whose lyrics contain words or a phrase, e.g. to answer \"which song says ...\". " +
			"Accent- and case-insensitive; exact phrase matches come first. Supports \"quoted phrases\", or, -word. " +
			"Result data items have track, snippet (matching lines), phrase_match and has_synced.",
		params: []param{{name: "q", maxLen: 200, required: true, desc: "Words or a phrase from the lyrics."},
			limitParam(10, 50)}},
	{name: "admin_translation_queue", title: "Lyrics translation queue (admin)", path: "/lyrics/translation-queue",
		desc: "The Claude lyrics translation queue: counts of queued, in_progress, done, skipped, refused, error " +
			"(retry scheduled) and given_up songs, next_retry_at, and calls_today / calls_month against daily_cap / " +
			"monthly_cap, tokens_month and the configured model. Songs are queued when they play."},
	{name: "admin_requeue_translation", title: "Re-queue a lyrics translation (admin)",
		path: "/tracks/{id}/translation/requeue", method: "POST",
		desc: "Put one track's lyrics translation at the front of the queue, clearing a refused, failed, given-up " +
			"or done translation. Fails when the track has no lyrics. Uses Claude credit when it runs.",
		params: []param{idParam("Spotify track ID.")}},
	{name: "admin_lyrics_queue", title: "Lyrics lookup queue (admin)", path: "/lyrics/queue",
		desc: "The LRCLIB lyrics lookup queue: counts of queued (requested = admin re-queues, backlog = never " +
			"looked up), in_progress, found, instrumental, not_found and error (retry scheduled) tracks, " +
			"next_retry_at and last_lookup_at. Order: the song playing now, upcoming songs, re-queues, " +
			"recently played, then the rest of the library."},
	{name: "admin_requeue_lyrics", title: "Re-queue a lyrics lookup (admin)",
		path: "/tracks/{id}/lyrics/requeue", method: "POST", destructive: true,
		desc: "Put one track's LRCLIB lyrics lookup at the front of the queue, whatever its status (not found, " +
			"failed, or found: stale lyrics are fetched again). When the lyrics change, an existing translation " +
			"is redone first in line (uses Claude credit). Fails when the track is unknown or the lyrics fetcher is off. " +
			"Lyrics added by hand (source manual) are kept: the call fails unless replace_manual is true. Marked destructive because replace_manual can replace them " +
			"(without it, the call only reorders the queue).",
		params: []param{idParam("Spotify track ID."),
			{name: "replace_manual", boolean: true, inBody: true,
				desc: "Let the next LRCLIB result replace lyrics added by hand (a lookup that finds nothing keeps them)."}}},
	{name: "admin_set_lyrics", title: "Set lyrics by hand (admin)",
		path: "/tracks/{id}/lyrics", method: "PUT", destructive: true,
		desc: "Store lyrics for one track by hand (source manual). They win over LRCLIB: automatic lookups never " +
			"replace them. Give synced LRC text (\"[mm:ss.xx] line\" per line), plain text, or both; with only synced " +
			"text the plain text is derived. Or set instrumental (no text). Synced text is checked line by line " +
			"(valid timestamps in time order, none past the song's end); errors name the line. When the lyrics " +
			"change, an existing translation is redone first in line (uses Claude credit). Destructive: it replaces the " +
			"stored lyrics (LRCLIB's or earlier manual ones). Each text is at most 200 000 bytes of UTF-8 (fewer " +
			"characters for non-ASCII text); a larger one fails with too_large. Returns the track lyrics.",
		params: []param{idParam("Spotify track ID."),
			{name: "synced", maxLen: 200_000, inBody: true, desc: "Time-synced LRC lyrics (at most 200 000 bytes of UTF-8)."},
			{name: "plain", maxLen: 200_000, inBody: true, desc: "Plain lyrics, derived from synced when left out (at most 200 000 bytes of UTF-8)."},
			{name: "instrumental", boolean: true, inBody: true, desc: "The song has no lyrics (give no text)."}}},
	{name: "admin_clear_manual_lyrics", title: "Remove lyrics set by hand (admin)",
		path: "/tracks/{id}/lyrics/manual", method: "DELETE", destructive: true,
		desc: "Remove the lyrics added by hand for one track and queue an LRCLIB lookup at the front. " +
			"Fails when the track has no manual lyrics. Returns the track lyrics.",
		params: []param{idParam("Spotify track ID.")}},
	{name: "admin_artists", title: "Artists (admin)", path: "/artists",
		desc: "Every artist with play and track counts. Result has data and total.",
		params: []param{
			searchParam("Search by artist name or a Spotify ID/URI."),
			{name: "sort", enum: []string{"plays", "name", "tracks"}, def: "plays", desc: "Sort column."},
			{name: "order", enum: sortOrders, def: "desc", desc: "Sort direction."},
			limitParam(25, 100), offsetParam(),
		}},
	{name: "admin_playlists", title: "Playlists (admin)", path: "/playlists",
		desc:   "Every playlist played from, public and private. Result has data and total.",
		params: []param{limitParam(25, 100), offsetParam()}},
	{name: "admin_top_playlists", title: "Top 50 playlists (admin)", path: "/top-playlists",
		desc: "Every generated Top 50 playlist row (all statuses) and the generator status. Result has data, total and status.",
		params: []param{
			{name: "period", enum: topPeriods, desc: "Only this period kind."},
			limitParam(50, 100), offsetParam(),
		}},
	{name: "admin_top_playlist", title: "Top 50 playlist (admin)", path: "/top-playlists/{period}/{start}",
		desc: "One generated Top 50 playlist with its track snapshot.",
		params: []param{
			{name: "period", enum: topPeriods, required: true, inPath: true, desc: "Period kind."},
			{name: "start", maxLen: 64, required: true, inPath: true,
				desc: "Period start as an RFC 3339 timestamp, exactly as in period_start of admin_top_playlists."},
		}},
	{name: "admin_backups", title: "Backups", path: "/backups",
		desc: "Database backup status: state, recent runs, stored backups, pending request and schedule settings."},
	{name: "admin_api_analytics", title: "API analytics", path: "/analytics",
		desc:   "Public API usage over a time window: requests, errors, routes, status codes and top origins.",
		params: analyticsParams()},
	{name: "admin_page_analytics", title: "Page analytics", path: "/analytics/pages",
		desc:   "Public page views: paths, devices, browsers, countries and referrers over a time window.",
		params: analyticsParams()},
}

func analyticsParams() []param {
	return []param{
		{name: "range", enum: analyticsRanges, def: "24h", desc: "Time window: the last 24 hours, 7 days or 30 days."},
		{name: "tz", maxLen: 64, desc: "IANA time zone for daily buckets, e.g. Europe/Berlin (default UTC)."},
	}
}

// ToolInfo names and describes one MCP tool (for the admin dashboard).
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PublicTools lists the tools served to every caller.
func PublicTools() []ToolInfo { return toolInfos(publicTools) }

// AdminTools lists the tools served only to callers with an admin token.
func AdminTools() []ToolInfo { return toolInfos(adminTools) }

func toolInfos(defs []toolDef) []ToolInfo {
	out := make([]ToolInfo, len(defs))
	for i, d := range defs {
		out[i] = ToolInfo{Name: d.name, Description: d.desc}
	}
	return out
}
