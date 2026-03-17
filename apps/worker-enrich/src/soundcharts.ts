import { z } from "zod";
import { SOUNDCHARTS_CREDENTIALS } from "./config.ts";

// ─── Soundcharts response schema ──────────────────────────────────────────────

const SoundchartsAudioSchema = z.object({
  acousticness: z.number(),
  danceability: z.number(),
  energy: z.number(),
  instrumentalness: z.number(),
  key: z.number().int(),
  liveness: z.number(),
  loudness: z.number(),
  mode: z.number().int(),
  speechiness: z.number(),
  tempo: z.number(),
  timeSignature: z.number().int(),
  valence: z.number(),
});

const SoundchartsGenreSchema = z.object({
  root: z.string(),
  sub: z.array(z.string()),
});

const SoundchartsSongSchema = z.object({
  type: z.literal("song"),
  object: z.object({
    audio: SoundchartsAudioSchema,
    genres: z.array(SoundchartsGenreSchema).optional(),
  }),
});

export type SoundchartsAudio = z.infer<typeof SoundchartsAudioSchema>;
export type SoundchartsGenre = z.infer<typeof SoundchartsGenreSchema>;
export type SoundchartsSongResult = {
  audio: SoundchartsAudio;
  genres?: SoundchartsGenre[];
};

// ─── Round-robin key rotation ─────────────────────────────────────────────────

let credentialIndex = 0;

function nextCredential() {
  const cred =
    SOUNDCHARTS_CREDENTIALS[credentialIndex % SOUNDCHARTS_CREDENTIALS.length];
  credentialIndex = (credentialIndex + 1) % SOUNDCHARTS_CREDENTIALS.length;
  return cred!;
}

// ─── API call ─────────────────────────────────────────────────────────────────

const SOUNDCHARTS_BASE = "https://customer.api.soundcharts.com";

export async function fetchAudioFeaturesByIsrc(
  isrc: string,
): Promise<SoundchartsSongResult | null> {
  const cred = nextCredential();
  const url = `${SOUNDCHARTS_BASE}/api/v2.25/song/by-isrc/${encodeURIComponent(isrc)}`;

  const response = await fetch(url, {
    headers: {
      "x-app-id": cred.appId,
      "x-api-key": cred.apiKey,
    },
  });

  if (response.status === 404) {
    return null;
  }

  if (!response.ok) {
    const body = await response.text();
    throw new Error(`Soundcharts request failed (${response.status}): ${body}`);
  }

  const json = await response.json();
  const parsed = SoundchartsSongSchema.safeParse(json);

  if (!parsed.success) {
    console.warn(
      `Unexpected Soundcharts response shape for ISRC ${isrc}:`,
      parsed.error.message,
    );
    return null;
  }

  return { audio: parsed.data.object.audio, genres: parsed.data.object.genres };
}
