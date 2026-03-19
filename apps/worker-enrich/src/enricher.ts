import type { TrackFeaturesRecord } from "@shared/schemas";
import { readItems, updateItem } from "./directus.ts";
import {
  fetchAudioFeaturesByIsrc,
  type SoundchartsSongResult,
} from "./soundcharts.ts";
import { info, warn, error } from "@shared/logger.ts";

// Shape returned by Directus when track_id relation is expanded.
type TrackFeaturesWithTrack = Omit<TrackFeaturesRecord, "track_id"> & {
  track_id: { id: string; isrc: string | null } | string;
};

export async function runEnrichCycle(): Promise<void> {
  const records = await readItems<TrackFeaturesWithTrack>("track_features", {
    // Primary filter to avoid returning already-enriched rows, but still
    // defensively check `enriched` at runtime to avoid races or stale data.
    filter: { enriched: { _eq: false } },
    limit: -1,
    fields: ["id", "isrc", "enriched", "track_id.id", "track_id.isrc"],
  });

  if (records.length === 0) {
    info("No unenriched track_features found.");
    return;
  }

  info(`Enriching ${records.length} record(s)…`);

  for (const record of records) {
    const recordId = record.id;
    if (!recordId) {
      warn("Skipping record with no id.");
      continue;
    }

    // Defensive guard: if the record has already been enriched (concurrent
    // worker or manual update), skip it.
    if (record.enriched) {
      info(`track_features[${recordId}] already enriched; skipping.`);
      continue;
    }

    // Resolve ISRC: prefer directly stored value, fall back to expanded track relation.
    const isrc: string | null | undefined =
      record.isrc ??
      (typeof record.track_id === "object" ? record.track_id?.isrc : null);

    if (!isrc) {
      warn(`track_features[${recordId}]: no ISRC available, skipping.`);
      continue;
    }

    try {
      const result: SoundchartsSongResult | null =
        await fetchAudioFeaturesByIsrc(isrc);

      if (result === null || !result.audio) {
        // No audio features available — mark as enriched with empty feature set
        warn(
          `track_features[${recordId}]: ISRC ${isrc} not found or no audio features; marking enriched with empty features.`,
        );

        await updateItem<TrackFeaturesRecord>("track_features", recordId, {
          acousticness: null,
          danceability: null,
          energy: null,
          instrumentalness: null,
          key: null,
          liveness: null,
          loudness: null,
          mode: null,
          speechiness: null,
          tempo: null,
          valence: null,
          genres: null,
          enriched: true,
          enriched_at: new Date().toISOString(),
        } as unknown as Partial<TrackFeaturesRecord>);

        info(
          `track_features[${recordId}] marked enriched (no audio features).`,
        );
        continue;
      }

      const { audio, genres } = result;

      await updateItem<TrackFeaturesRecord>("track_features", recordId, {
        acousticness: audio.acousticness,
        danceability: audio.danceability,
        energy: audio.energy,
        instrumentalness: audio.instrumentalness,
        key: audio.key,
        liveness: audio.liveness,
        loudness: audio.loudness,
        mode: audio.mode,
        speechiness: audio.speechiness,
        tempo: audio.tempo,
        valence: audio.valence,
        genres: genres ?? null,
        enriched: true,
        enriched_at: new Date().toISOString(),
      });

      info(`track_features[${recordId}] enriched (ISRC: ${isrc}).`);
    } catch (err) {
      error(`track_features[${recordId}] enrichment failed:`, err);
    }
  }
}
