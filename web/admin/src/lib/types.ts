// Types for every shape in docs/admin-api.md (the source of truth) plus the
// public /api/v1 read endpoints the dashboard consumes.

// ---------- shared ----------

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
	uri?: string;
	external_url?: string;
};

export type TrackView = {
	id: string;
	name: string;
	uri?: string;
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

export type EnrichmentStatus = 'pending' | 'enriched' | 'unavailable' | 'failed';
export const ENRICHMENT_STATUSES: readonly EnrichmentStatus[] = [
	'pending',
	'enriched',
	'unavailable',
	'failed'
];

export type AudioFeatures = {
	acousticness: number | null;
	danceability: number | null;
	energy: number | null;
	instrumentalness: number | null;
	liveness: number | null;
	loudness: number | null;
	speechiness: number | null;
	tempo: number | null;
	valence: number | null;
	key: number | null;
	mode: number | null;
	time_signature: number | null;
};

export type Genre = { root: string; sub: string[] };

/** `{ data, total }` (offset lists) or `{ data, next_cursor }` (cursor lists). */
export type OffsetList<T> = { data: T[]; total: number };
export type CursorList<T> = { data: T[]; next_cursor?: string | null };

// ---------- auth ----------

export type AdminConfig = { clerk_publishable_key: string; version: string };

export type AdminUser = {
	id: string;
	email: string;
	name: string;
	image_url: string;
};

export type MeResponse = { user: AdminUser };

// ---------- overview ----------

export type Summary = {
	total_plays: number;
	unique_tracks: number;
	unique_artists: number;
	listening_ms: number;
	first_played_at: string | null;
	last_played_at: string | null;
	enriched_tracks: number;
};

export type EnrichmentCounts = Record<EnrichmentStatus, number>;

export type ServiceStatus = 'ok' | 'degraded' | 'down';

export type ServiceHealth = {
	name: string;
	status: ServiceStatus;
	last_seen_at?: string | null;
	detail?: string | null;
};

export type Overview = {
	now_playing: NowPlaying | null;
	summary_all: Summary;
	summary_7d: Summary;
	enrichment: EnrichmentCounts;
	spotify: SpotifyStatus;
	services: ServiceHealth[];
	version: string;
};

// ---------- spotify ----------

export type SpotifyStatus = {
	connected: boolean;
	source: 'env' | 'dashboard';
	authorized_at: string | null;
	expires_at: string | null;
	days_left: number | null;
	lifetime_days: number;
	scopes: string[];
	redirect_uri: string;
	client_id: string;
	last_refresh_at: string | null;
	last_error: string | null;
	account: { id: string; display_name: string } | null;
};

export type SpotifyAuthorizeResponse = { url: string };

export type SpotifyTestResponse =
	{ ok: true; status: number; now_playing: NowPlaying | null } | { ok: false; error: string };

// ---------- email ----------

export type EmailSettings = {
	host: string;
	port: number;
	username: string;
	from: string;
	alert_to: string[];
	password_set: boolean;
	enabled: boolean;
	expiry_alert_days: number[];
	updated_at: string | null;
};

export type EmailSettingsUpdate = {
	host: string;
	port: number;
	username: string;
	from: string;
	alert_to: string[];
	expiry_alert_days: number[];
	/** Omit to keep the stored password, `""` to clear it. */
	password?: string;
};

export type OkResult = { ok: true } | { ok: false; error: string };

// ---------- data ----------

export type HistoryEntry = {
	id: string;
	played_at: string;
	updated_at: string;
	replay_count: number;
	track: TrackView;
};

export type TrackSort = 'plays' | 'last_played' | 'name' | 'created';
export type ArtistSort = 'plays' | 'name' | 'tracks';
export type SortOrder = 'asc' | 'desc';

export type TrackRow = {
	track: TrackView;
	plays: number;
	last_played: string | null;
	enrichment_status: EnrichmentStatus;
	created_at: string;
};

export type TrackEnrichment = {
	status: EnrichmentStatus;
	source: string | null;
	attempts: number;
	last_error: string | null;
	next_attempt_at: string | null;
	enriched_at: string | null;
	features: AudioFeatures | null;
	genres: Genre[] | null;
};

export type TrackAdminDetail = TrackView & {
	plays: number;
	last_played: string | null;
	created_at: string;
	enrichment: TrackEnrichment | null;
};

export type ArtistRow = {
	artist: Artist;
	plays: number;
	tracks: number;
	last_played: string | null;
};

// ---------- analytics ----------

export type AnalyticsRange = '24h' | '7d' | '30d';

export type Analytics = {
	range: AnalyticsRange;
	bucket: 'hour' | 'day';
	/** IANA zone the daily buckets were computed in (echo of `tz`). */
	time_zone?: string;
	totals: {
		requests: number;
		errors: number;
		error_rate: number;
		p50_ms: number;
		p95_ms: number;
		p99_ms: number;
		unique_clients: number;
	};
	timeseries: { bucket: string; requests: number; errors: number; p95_ms: number }[];
	routes: {
		route: string;
		method: string;
		requests: number;
		errors: number;
		p50_ms: number;
		p95_ms: number;
		avg_ms: number;
	}[];
	status_codes: { code: number; count: number }[];
	top_origins: { origin: string; count: number }[];
};

// ---------- public page analytics ----------

/** One ranked dimension value. */
export type DimCount = { name: string; views: number };

/** Cookieless visitor analytics for the public page. */
export type PageAnalytics = {
	range: AnalyticsRange;
	bucket: 'hour' | 'day';
	/** IANA zone the daily buckets were computed in (echo of `tz`). */
	time_zone: string;
	totals: {
		views: number;
		/** Unique per UTC day (cookieless, salted daily hash), de-duplicated across the range. */
		visitors: number;
		avg_visible_ms: number;
		/** Visitors seen in the last 5 minutes. */
		live_now: number;
	};
	/** Zero-filled, oldest first. */
	timeseries: { bucket: string; views: number }[];
	paths: DimCount[];
	/** `''` = direct (no referrer). */
	referrers: DimCount[];
	devices: DimCount[];
	browsers: DimCount[];
	/** ISO 3166-1 alpha-2; `''` = unknown. */
	countries: DimCount[];
};

// ---------- audit ----------

export type AuditEntry = {
	id: string;
	at: string;
	actor: string;
	action: string;
	detail: string | Record<string, unknown> | null;
	ip: string | null;
};

// ---------- public /api/v1 stats ----------

export type StatsRange = '7d' | '30d' | '90d' | '365d' | 'all';

export type V1Item<T> = { data: T; range?: string };
export type V1List<T> = { data: T[]; range?: string; next_cursor?: string };

export type TopTrack = { track: TrackView; plays: number; last_played: string };
export type TopArtist = { artist: Artist; plays: number; last_played: string };
export type GenreStat = {
	genre: string;
	plays: number;
	share: number;
	sub: { genre: string; plays: number }[];
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

// ---------- playlists ----------

export type Playlist = {
	id: string;
	/** false for Spotify-generated mixes / playlists the app can't read (no name/cover). */
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

// ---------- time zone (server authority, /api/v1/timezone) ----------

export type ServerZone = {
	tz: string;
	abbreviation: string;
	/** Minutes east of UTC, e.g. -360 for UTC−06:00. */
	offset_minutes: number;
	/** ISO offset, e.g. "-06:00". */
	offset: string;
	now: string;
	source: string;
};

// ---------- top 50 playlists (admin) ----------

export type TopPlaylistPeriod = 'week' | 'month' | 'year';
export const TOP_PLAYLIST_PERIODS: readonly TopPlaylistPeriod[] = ['week', 'month', 'year'];

export type TopPlaylistStatus = 'pending' | 'created' | 'empty' | 'waiting_auth' | 'failed';

export type AdminTopPlaylist = {
	period: TopPlaylistPeriod;
	/** RFC 3339 in the row's zone; sent back verbatim (URL-encoded) as `{start}`. */
	period_start: string;
	period_end: string;
	time_zone: string;
	/** "Week of Nov 2, 2026" */
	label: string;
	name: string;
	status: TopPlaylistStatus;
	hidden: boolean;
	spotify_id: string | null;
	spotify_url: string | null;
	track_count: number;
	total_plays: number;
	attempts: number;
	last_error: string | null;
	next_attempt_at: string | null;
	published_at: string | null;
	/** The current, unfinished period: updated while it runs, finalized in place when it ends. */
	live: boolean;
	/** RFC 3339; last live re-rank or finalize (null if never). */
	refreshed_at: string | null;
	created_at: string;
	updated_at: string;
};

/** One position of a stored snapshot (same JSON as the public endpoint). */
export type TopPlaylistTrack = { position: number; plays: number; track: TrackView };

export type AdminTopPlaylistDetail = AdminTopPlaylist & { tracks: TopPlaylistTrack[] };

export type TopPlaylistsStatus = {
	enabled: boolean;
	/** Enabled kinds; [] when the feature is disabled. */
	periods: TopPlaylistPeriod[];
	/** Configured zone ("UTC" when disabled). */
	time_zone: string;
	/** Spotify connected and granted playlist-modify-public. */
	authorized: boolean;
	interval_seconds: number;
	/** Kinds that get a live playlist for the running period; [] when off. */
	live_periods: TopPlaylistPeriod[];
	/** How often a live playlist is re-ranked. */
	live_refresh_seconds: number;
};

export type TopPlaylistsList = OffsetList<AdminTopPlaylist> & { status: TopPlaylistsStatus };
