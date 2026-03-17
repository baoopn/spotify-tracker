const DEFAULT_ENRICH_INTERVAL_MS = 5 * 60 * 1_000; // 5 minutes

// Allow overriding the interval (milliseconds) from env var `ENRICH_INTERVAL_MS`.
// Must be a positive number representing milliseconds.
const rawInterval = Deno.env.get("ENRICH_INTERVAL_MS");
let ENRICH_INTERVAL_MS = DEFAULT_ENRICH_INTERVAL_MS;
if (rawInterval !== undefined) {
  const parsed = Number(rawInterval);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new Error(
      "Invalid ENRICH_INTERVAL_MS env var; must be a positive number (milliseconds).",
    );
  }
  ENRICH_INTERVAL_MS = Math.floor(parsed);
}

export { ENRICH_INTERVAL_MS };

// ─── Directus ─────────────────────────────────────────────────────────────────

const directusUrl = Deno.env.get("DIRECTUS_URL");
const directusToken =
  Deno.env.get("DIRECTUS_TOKEN_WORKER_ENRICH") ??
  Deno.env.get("DIRECTUS_TOKEN");

if (!directusUrl) throw new Error("Missing DIRECTUS_URL env var.");
if (!directusToken) {
  throw new Error(
    "Missing Directus token env var. Set DIRECTUS_TOKEN_WORKER_ENRICH or DIRECTUS_TOKEN.",
  );
}

export const DIRECTUS_URL = directusUrl;
export const DIRECTUS_TOKEN = directusToken;

// ─── Soundcharts credential rotation ─────────────────────────────────────────

interface SoundchartsCredential {
  appId: string;
  apiKey: string;
}

function loadSoundchartsCredentials(): SoundchartsCredential[] {
  const creds: SoundchartsCredential[] = [];

  // First pair has no suffix, subsequent pairs are numbered _2, _3, …
  const suffixes = ["", ...Array.from({ length: 20 }, (_, i) => `_${i + 2}`)];

  for (const suffix of suffixes) {
    const appId = Deno.env.get(`SOUNDCHARTS_APP_ID${suffix}`);
    const apiKey = Deno.env.get(`SOUNDCHARTS_TOKEN${suffix}`);

    if (!appId && !apiKey) {
      // No more credential pairs; stop scanning.
      break;
    }
    if (!appId || !apiKey) {
      throw new Error(
        `Incomplete Soundcharts credentials for suffix "${suffix}": both SOUNDCHARTS_APP_ID${suffix} and SOUNDCHARTS_TOKEN${suffix} must be set.`,
      );
    }

    creds.push({ appId, apiKey });
  }

  if (creds.length === 0) {
    throw new Error(
      "No Soundcharts credentials found. Set at least SOUNDCHARTS_APP_ID and SOUNDCHARTS_TOKEN.",
    );
  }

  return creds;
}

export const SOUNDCHARTS_CREDENTIALS = loadSoundchartsCredentials();
