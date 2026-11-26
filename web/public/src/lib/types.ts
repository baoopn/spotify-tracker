// Public API (/api/v1) response shapes.

export type Artist = {
	id: string;
	name: string;
	uri?: string;
	external_url?: string;
	image_url?: string;
};

export type Album = {
	id: string;
	name: string;
	release_date?: string;
	cover_image_url?: string;
	external_url?: string;
};

export type TrackView = {
	id: string;
	name: string;
	isrc?: string;
	duration_ms: number;
	explicit: boolean;
	external_url?: string;
	album?: Album;
	artists: Artist[];
};

export type NowPlaying = {
	is_playing: boolean;
	progress_ms: number | null;
	fetched_at: string;
	track: TrackView | null;
};

/** One synced lyric line; empty text marks an instrumental gap. */
export type LyricLine = { start_ms: number; text: string };

/** One language of a lyrics translation: `lines` line up with the synced
 * lines (same length, "" for empty lines); `plain` for plain lyrics. */
export type LyricsTranslationText = { lines: string[]; plain: string | null };

/** A song title or album name in each language; a null language stays as
 * written (already in that language, or only a name). */
export type TranslatedName = { en: string | null; vi: string | null };

/** The Claude translation of found lyrics. A null language means the song
 * is already in that language (or there is none). */
export type LyricsTranslation = {
	status: 'pending' | 'done';
	source_language: string | null;
	/** Every language in the lyrics, main first; null when not recorded. */
	languages: string[] | null;
	en: LyricsTranslationText | null;
	vi: LyricsTranslationText | null;
	/** The song title and album name; null when neither language has one
	 * (also while pending and for older translations). */
	title?: TranslatedName | null;
	album?: TranslatedName | null;
	credit: { text: string };
};

/**
 * The lyrics of the track playing now (GET /api/v1/now-playing/lyrics).
 * Status `found`: `synced` lyrics come as timed `lines`, otherwise `plain`
 * holds the text. `instrumental` and `pending` (looked up soon) have neither.
 */
export type NowLyrics = {
	track_id: string;
	status: 'found' | 'instrumental' | 'pending';
	synced: boolean;
	instrumental: boolean;
	lines: LyricLine[];
	plain: string | null;
	/** url is null for lyrics added by hand ("Lyrics added by Bao"). */
	credit: { text: string; url: string | null };
	translation: LyricsTranslation | null;
};

export type HistoryEntry = {
	id: string;
	played_at: string;
	updated_at: string;
	replay_count: number;
	track: TrackView;
};

export type Playlist = {
	id: string;
	available: boolean;
	name?: string;
	description?: string;
	owner_id?: string;
	owner_name?: string;
	cover_image_url?: string;
	external_url?: string;
	uri?: string;
	public: boolean | null;
	collaborative: boolean;
	tracks_total: number | null;
	fetched_at: string;
};

export type RecentPlaylist = { playlist: Playlist; plays: number; last_played: string };

export type TopPeriod = 'week' | 'month' | 'year';

export type TopPlaylistTrack = { position: number; plays: number; track: TrackView };

/**
 * An auto-generated "Top 50" playlist for one week, month or year: a completed period, or
 * (when `live`) the current one, updated while it runs and finalized in place when it ends.
 */
export type TopPlaylist = {
	period: TopPeriod;
	/** RFC 3339 instant; the period is half-open [period_start, period_end). */
	period_start: string;
	period_end: string;
	/** IANA zone this period was generated in (its boundaries and label). */
	time_zone: string;
	label: string;
	name: string;
	spotify_id: string;
	spotify_url: string;
	track_count: number;
	total_plays: number;
	published_at: string;
	/** The current, unfinished period ("so far"); `period_end` is still the period's end. */
	live: boolean;
	/** RFC 3339; when the live playlist was last re-ranked (null if never). */
	refreshed_at: string | null;
	/** Filled only for the newest entry of each period; older entries have []. */
	tracks: TopPlaylistTrack[];
};

export type TopPlaylists = {
	/** IANA zone the period boundaries are defined in. */
	time_zone: string;
	/** Newest first; every key is always present (possibly empty). */
	periods: Record<TopPeriod, TopPlaylist[]>;
};

export type AudioFeatureValues = {
	acousticness: number | null;
	danceability: number | null;
	energy: number | null;
	instrumentalness: number | null;
	liveness: number | null;
	speechiness: number | null;
	valence: number | null;
	/** dB, about -60..0 */
	loudness: number | null;
	/** BPM */
	tempo: number | null;
	/** 0 = C .. 11 = B, -1 = none */
	key: number | null;
	/** 1 major, 0 minor */
	mode: number | null;
	time_signature: number | null;
};

export type EnrichmentStatus = 'pending' | 'enriched' | 'unavailable' | 'failed';

export type TrackFeatures = {
	status: EnrichmentStatus;
	source?: string;
	enriched_at: string | null;
	genres: { root: string; sub: string[] }[];
	features: AudioFeatureValues;
};

export type TrackDetail = TrackView & {
	plays: number;
	last_played: string | null;
	features: TrackFeatures | null;
};

export type AudioProfile = {
	sample_tracks: number;
	sample_plays: number;
	acousticness: number | null;
	danceability: number | null;
	energy: number | null;
	instrumentalness: number | null;
	liveness: number | null;
	loudness: number | null;
	speechiness: number | null;
	tempo: number | null;
	valence: number | null;
};

/** Range of the rolling top lists: last 7 days, 30 days or 12 months. */
export type StatsRange = '7d' | '30d' | '365d';

export type TopTrack = { plays: number; last_played: string; track: TrackView };
export type TopArtist = { plays: number; last_played: string; artist: Artist };

export type ItemResponse<T> = { data: T };
export type ListResponse<T> = { data: T[]; next_cursor?: string };
export type ErrorResponse = { error: { code: string; message: string } };
