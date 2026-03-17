import { z } from "zod";

// ─── Entity schemas (mirror Directus collections) ────────────────────────────

/** `artists` collection — Spotify artist_id is the primary key. */
export const ArtistSchema = z.object({
  id: z.string(),
  name: z.string(),
  uri: z.string().optional(),
  external_url: z.string().optional(),
});

/** `albums` collection — Spotify album_id is the primary key. */
export const AlbumSchema = z.object({
  id: z.string(),
  name: z.string(),
  release_date: z.string().optional(),
  cover_image: z.string().optional(), // primary image URL
  uri: z.string().optional(),
  external_url: z.string().optional(),
});

/**
 * `tracks` collection — Spotify track_id is the primary key.
 * `album_id` is an M2O FK → `albums`.
 * Artists are linked via M2M through the `track_artists` junction.
 */
export const TrackSchema = z.object({
  id: z.string(),
  name: z.string(),
  uri: z.string().optional(),
  isrc: z.string().optional(),
  duration_ms: z.number().int().nonnegative(),
  explicit: z.boolean().optional(),
  external_url: z.string().optional(),
  album_id: z.string().optional(),
});

/** `tracks` with relations hydrated (used for Directus reads with `fields: ["*.*"]`). */
export const TrackPopulatedSchema = TrackSchema.extend({
  album: AlbumSchema.optional(),
  artists: z.array(ArtistSchema).optional(),
});

// ─── Audio Features ───────────────────────────────────────────────────────────

export const AudioFeaturesSchema = z.object({
  isrc: z.string().optional(),
  acousticness: z.number(),
  danceability: z.number(),
  energy: z.number(),
  instrumentalness: z.number(),
  key: z.number().int().optional(),
  liveness: z.number(),
  loudness: z.number(),
  mode: z.number().int().optional(),
  speechiness: z.number(),
  tempo: z.number(),
  valence: z.number(),
});

// ─── Directus collection schemas ─────────────────────────────────────────────

/** Write schema — `track_id` is the M2O FK → `tracks`. */
export const CurrentListeningSchema = z.object({
  id: z.string().optional(),
  track_id: z.string(),
  is_playing: z.boolean().optional(),
  progress_ms: z.number().nullable().optional(),
  fetched_at: z.string().optional(),
});

/** Read schema — `track` is the hydrated relation. */
export const CurrentListeningPopulatedSchema = CurrentListeningSchema.extend({
  track: TrackPopulatedSchema.optional(),
});

/** Write schema — `track_id` is the M2O FK → `tracks`. */
export const ListeningHistorySchema = z.object({
  id: z.string().optional(),
  track_id: z.string(),
  played_at: z.string(),
  updated_at: z.string().optional(),
  duration_ms: z.number().int().optional(),
  replay_count: z.number().int().min(1).optional().default(1),
});

/** Read schema — `track` is the hydrated relation. */
export const ListeningHistoryPopulatedSchema = ListeningHistorySchema.extend({
  track: TrackPopulatedSchema.optional(),
});

/** `track_id` is an O2O FK → `tracks` (enforced unique in Directus). Audio feature fields are provider-agnostic (Soundcharts-ready). */
export const TrackFeaturesRecordSchema = z.object({
  id: z.string().optional(),
  track_id: z.string(),
  isrc: z.string().nullable().optional(),
  // Genres as returned by Soundcharts: array of { root, sub[] }
  genres: z
    .array(z.object({ root: z.string(), sub: z.array(z.string()) }))
    .nullable()
    .optional(),
  acousticness: z.number().nullable().optional(),
  danceability: z.number().nullable().optional(),
  energy: z.number().nullable().optional(),
  instrumentalness: z.number().nullable().optional(),
  key: z.number().int().nullable().optional(),
  liveness: z.number().nullable().optional(),
  loudness: z.number().nullable().optional(),
  mode: z.number().int().nullable().optional(),
  speechiness: z.number().nullable().optional(),
  tempo: z.number().nullable().optional(),
  valence: z.number().nullable().optional(),
  enriched: z.boolean().optional().default(false),
  enriched_at: z.string().optional(),
});

export type Artist = z.infer<typeof ArtistSchema>;
export type Album = z.infer<typeof AlbumSchema>;
export type Track = z.infer<typeof TrackSchema>;
export type TrackPopulated = z.infer<typeof TrackPopulatedSchema>;
export type AudioFeatures = z.infer<typeof AudioFeaturesSchema>;
export type CurrentListening = z.infer<typeof CurrentListeningSchema>;
export type CurrentListeningPopulated = z.infer<
  typeof CurrentListeningPopulatedSchema
>;
export type ListeningHistory = z.infer<typeof ListeningHistorySchema>;
export type ListeningHistoryPopulated = z.infer<
  typeof ListeningHistoryPopulatedSchema
>;
export type TrackFeaturesRecord = z.infer<typeof TrackFeaturesRecordSchema>;
