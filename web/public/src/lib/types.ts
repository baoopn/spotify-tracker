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

export type ItemResponse<T> = { data: T };
export type ListResponse<T> = { data: T[]; next_cursor?: string };
export type ErrorResponse = { error: { code: string; message: string } };
