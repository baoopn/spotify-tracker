const SPOTIFY_NOW_PLAYING_ENDPOINT = `https://api.spotify.com/v1/me/player/currently-playing`;
const SPOTIFY_TOKEN_ENDPOINT = `https://accounts.spotify.com/api/token`;

import { z } from "zod";
import type { Album, Artist, Track } from "@shared/schemas";

const SpotifyArtistSchema = z.object({
  id: z.string(),
  name: z.string(),
  uri: z.string().optional(),
  external_urls: z.object({ spotify: z.string().optional() }).optional(),
});

const SpotifyAlbumSchema = z.object({
  id: z.string(),
  name: z.string(),
  release_date: z.string().optional(),
  uri: z.string().optional(),
  external_urls: z.object({ spotify: z.string().optional() }).optional(),
  images: z.array(z.object({ url: z.string() })).optional(),
});

const SpotifyTrackSchema = z.object({
  id: z.string(),
  name: z.string(),
  uri: z.string().optional(),
  external_ids: z.object({ isrc: z.string().optional() }).optional(),
  duration_ms: z.number().int().nonnegative(),
  explicit: z.boolean().optional(),
  external_urls: z.object({ spotify: z.string().optional() }).optional(),
  album: SpotifyAlbumSchema,
  artists: z.array(SpotifyArtistSchema),
});

const SpotifyCurrentlyPlayingSchema = z.object({
  timestamp: z.number().int().optional(),
  is_playing: z.boolean().optional(),
  progress_ms: z.number().int().nullable().optional(),
  item: SpotifyTrackSchema.nullish(),
});

export interface CurrentlyPlayingSnapshot {
  track: Track;
  album: Album;
  artists: Artist[];
  isPlaying: boolean;
  progressMs: number | null;
  playedAt: string;
  fetchedAt: string;
}

// ─── Access Token Cache ───────────────────────────────────────────────────────

interface TokenCache {
  accessToken: string;
  scope: string; // space-separated OAuth scopes granted by refresh token
  expiresAt: number; // milliseconds since epoch
}

let tokenCache: TokenCache | null = null;

/**
 * Fetch a fresh access token from Spotify using refresh token grant.
 * Uses internal in-process cache to avoid redundant requests within token lifetime.
 *
 * Requires env vars: SPOTIFY_CLIENT_ID, SPOTIFY_CLIENT_SECRET, SPOTIFY_REFRESH_TOKEN
 *
 * @returns Promise<string> - Spotify access token (bearer token)
 * @throws Error if token request fails or env vars missing
 */
export async function getSpotifyAccessToken(): Promise<string> {
  const clientId = Deno.env.get("SPOTIFY_CLIENT_ID");
  const clientSecret = Deno.env.get("SPOTIFY_CLIENT_SECRET");
  const refreshToken = Deno.env.get("SPOTIFY_REFRESH_TOKEN");

  if (!clientId || !clientSecret || !refreshToken) {
    throw new Error(
      "Missing Spotify credentials: SPOTIFY_CLIENT_ID, SPOTIFY_CLIENT_SECRET, SPOTIFY_REFRESH_TOKEN",
    );
  }

  // Return cached token if valid (refresh 60s before expiry for safety)
  if (tokenCache) {
    const now = Date.now();
    const refreshThreshold = 60 * 1000; // 60 seconds
    if (now < tokenCache.expiresAt - refreshThreshold) {
      return tokenCache.accessToken;
    }
  }

  // Fetch new token
  const auth = btoa(`${clientId}:${clientSecret}`);
  const response = await fetch(SPOTIFY_TOKEN_ENDPOINT, {
    method: "POST",
    headers: {
      Authorization: `Basic ${auth}`,
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: new URLSearchParams({
      grant_type: "refresh_token",
      refresh_token: refreshToken,
    }).toString(),
  });

  if (!response.ok) {
    const errorBody = await response.text();
    throw new Error(
      `Spotify token request failed (${response.status}): ${errorBody}`,
    );
  }

  const data = (await response.json()) as {
    access_token: string;
    expires_in: number;
    scope?: string;
  };

  // Cache token with expiry time and scope
  tokenCache = {
    accessToken: data.access_token,
    scope: data.scope ?? "",
    expiresAt: Date.now() + data.expires_in * 1000,
  };

  return tokenCache.accessToken;
}

/**
 * Get the space-separated OAuth scopes of the cached access token.
 * Returns empty string if no token is cached or scope is unavailable.
 *
 * @returns string - space-separated scope list (e.g., "user-read-playback-state user-read-currently-playing")
 */
export function getCachedScopes(): string {
  return tokenCache?.scope ?? "";
}

// ─── Fetch Currently Playing ────────────────────────────────────────────────

/**
 * Fetch the currently-playing track from Spotify.
 *
 * Requires scope: `user-read-currently-playing`
 *
 * @returns Promise<CurrentlyPlayingSnapshot | null> - Currently-playing payload mapped to local entities
 * @throws Error if required scope is missing, token fetch fails, or API returns error
 */
export async function getCurrentlyPlayingTrack(): Promise<CurrentlyPlayingSnapshot | null> {
  const requiredScope = "user-read-currently-playing";
  const token = await getSpotifyAccessToken();
  const scopes = getCachedScopes();

  if (!scopes.split(" ").includes(requiredScope)) {
    throw new Error(
      `Missing required scope: "${requiredScope}". Current scopes: [${scopes}]`,
    );
  }

  const response = await fetch(SPOTIFY_NOW_PLAYING_ENDPOINT, {
    headers: {
      Authorization: `Bearer ${token}`,
    },
  });

  // 204 No Content: user is not playing anything
  if (response.status === 204) {
    return null;
  }

  if (!response.ok) {
    const errorBody = await response.text();
    throw new Error(
      `Spotify currently-playing fetch failed (${response.status}): ${errorBody}`,
    );
  }

  const data = SpotifyCurrentlyPlayingSchema.parse(await response.json());

  // If item is null or missing, nothing is playing
  if (!data.item) {
    return null;
  }

  const album: Album = {
    id: data.item.album.id,
    name: data.item.album.name,
    release_date: data.item.album.release_date,
    cover_image: data.item.album.images?.[0]?.url,
    uri: data.item.album.uri,
    external_url: data.item.album.external_urls?.spotify,
  };

  const artists: Artist[] = data.item.artists.map((artist) => ({
    id: artist.id,
    name: artist.name,
    uri: artist.uri,
    external_url: artist.external_urls?.spotify,
  }));

  const track: Track = {
    id: data.item.id,
    name: data.item.name,
    uri: data.item.uri,
    isrc: data.item.external_ids?.isrc,
    duration_ms: data.item.duration_ms,
    explicit: data.item.explicit,
    external_url: data.item.external_urls?.spotify,
    album_id: album.id,
  };

  const playedAt = new Date(data.timestamp ?? Date.now()).toISOString();
  const fetchedAt = new Date().toISOString();

  return {
    track,
    album,
    artists,
    isPlaying: data.is_playing ?? false,
    progressMs: data.progress_ms ?? null,
    playedAt,
    fetchedAt,
  };
}
