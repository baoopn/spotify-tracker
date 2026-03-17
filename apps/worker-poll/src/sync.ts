import type {
  Album,
  Artist,
  CurrentListening,
  Track,
  TrackFeaturesRecord,
} from "@shared/schemas";
import { getCurrentlyPlayingTrack } from "./spotify.ts";
import {
  createItem,
  readItems,
  updateItem,
  updateSingletonItem,
} from "./directus.ts";

type TrackCreatePayload = Track & {
  artists: Array<{ artists_id: string }>;
};

interface ListeningHistoryRecord {
  id?: string;
  track_id: string;
  replay_count?: number;
}

const REPLAY_PROGRESS_RESET_THRESHOLD_MS = 10_000;

let lastTrackId: string | null = null;
let lastProgressMs: number | null = null;

async function upsertArtist(artist: Artist): Promise<void> {
  const existing = await readItems<Artist>("artists", {
    filter: { id: { _eq: artist.id } },
    limit: 1,
  });

  if (existing.length > 0) {
    await updateItem<Artist>("artists", artist.id, artist);
    return;
  }

  await createItem<Artist>("artists", artist);
}

async function upsertAlbum(album: Album): Promise<void> {
  const existing = await readItems<Album>("albums", {
    filter: { id: { _eq: album.id } },
    limit: 1,
  });

  if (existing.length > 0) {
    await updateItem<Album>("albums", album.id, album);
    return;
  }

  await createItem<Album>("albums", album);
}

async function upsertTrack(track: Track, artists: Artist[]): Promise<void> {
  const artistLinks = artists.map((artist) => ({ artists_id: artist.id }));

  const existing = await readItems<Track>("tracks", {
    filter: { id: { _eq: track.id } },
    limit: 1,
  });

  if (existing.length > 0) {
    await updateItem<Track>("tracks", track.id, track);
    return;
  }

  await createItem<TrackCreatePayload>("tracks", {
    ...track,
    artists: artistLinks,
  });
}

async function upsertCurrentListening(record: CurrentListening): Promise<void> {
  await updateSingletonItem<CurrentListening>("current_listening", record);
}

async function ensureTrackFeaturesRecord(track: Track): Promise<void> {
  const existing = await readItems<TrackFeaturesRecord>("track_features", {
    filter: { track_id: { _eq: track.id } },
    fields: ["id"],
    limit: 1,
  });

  if (existing.length > 0) {
    return;
  }

  await createItem<Pick<TrackFeaturesRecord, "track_id" | "isrc">>(
    "track_features",
    {
      track_id: track.id,
      isrc: track.isrc ?? null,
    },
  );
}

async function incrementReplayCount(
  trackId: string,
  fetchedAt: string,
): Promise<boolean> {
  const latest = await readItems<ListeningHistoryRecord>("listening_history", {
    limit: 1,
    sort: ["-played_at"],
    fields: ["id", "track_id", "replay_count"],
  });

  const latestRecord = latest[0];
  if (!latestRecord?.id || latestRecord.track_id !== trackId) {
    return false;
  }

  await updateItem("listening_history", latestRecord.id, {
    replay_count: (latestRecord.replay_count ?? 1) + 1,
    updated_at: fetchedAt,
  });

  return true;
}

export async function syncCurrentlyPlaying(): Promise<void> {
  const snapshot = await getCurrentlyPlayingTrack();

  if (!snapshot) {
    console.log("No currently playing track.");
    return;
  }

  for (const artist of snapshot.artists) {
    await upsertArtist(artist);
  }

  await upsertAlbum(snapshot.album);
  await upsertTrack(snapshot.track, snapshot.artists);
  await ensureTrackFeaturesRecord(snapshot.track);

  await upsertCurrentListening({
    track_id: snapshot.track.id,
    is_playing: snapshot.isPlaying,
    progress_ms: snapshot.progressMs,
    fetched_at: snapshot.fetchedAt,
  });

  const hasProgressReset =
    lastTrackId === snapshot.track.id &&
    lastProgressMs !== null &&
    snapshot.progressMs !== null &&
    snapshot.progressMs + REPLAY_PROGRESS_RESET_THRESHOLD_MS < lastProgressMs;

  if (hasProgressReset) {
    const incremented = await incrementReplayCount(
      snapshot.track.id,
      snapshot.fetchedAt,
    );

    if (incremented) {
      console.log(`Incremented replay_count for track ${snapshot.track.id}`);
    }
  }

  if (lastTrackId !== snapshot.track.id) {
    await createItem("listening_history", {
      track_id: snapshot.track.id,
      played_at: snapshot.playedAt,
      updated_at: snapshot.fetchedAt,
      duration_ms: snapshot.track.duration_ms,
      replay_count: 1,
    });

    lastTrackId = snapshot.track.id;
    console.log(`Inserted listening history for track ${snapshot.track.id}`);
  }

  lastTrackId = snapshot.track.id;
  lastProgressMs = snapshot.progressMs;

  console.log(
    `Synced current listening: ${snapshot.track.name} (${snapshot.track.id})`,
  );
}
