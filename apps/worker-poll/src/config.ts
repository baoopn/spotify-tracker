const DEFAULT_POLL_INTERVAL_MS = 5_000; // 5 seconds

// Allow overriding the poll interval (milliseconds) from env var `POLL_INTERVAL_MS`.
// Must be a positive number representing milliseconds.
const rawPollInterval = Deno.env.get("POLL_INTERVAL_MS");
let POLL_INTERVAL_MS = DEFAULT_POLL_INTERVAL_MS;
if (rawPollInterval !== undefined) {
  const parsed = Number(rawPollInterval);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new Error(
      "Invalid POLL_INTERVAL_MS env var; must be a positive number (milliseconds).",
    );
  }
  POLL_INTERVAL_MS = Math.floor(parsed);
}

export { POLL_INTERVAL_MS };

const directusUrl = Deno.env.get("DIRECTUS_URL");
const directusToken =
  Deno.env.get("DIRECTUS_TOKEN_WORKER_POLL") ?? Deno.env.get("DIRECTUS_TOKEN");

if (!directusUrl) {
  throw new Error("Missing DIRECTUS_URL env var.");
}

if (!directusToken) {
  throw new Error(
    "Missing Directus token env var. Set DIRECTUS_TOKEN_WORKER_POLL or DIRECTUS_TOKEN.",
  );
}

export const DIRECTUS_URL = directusUrl;
export const DIRECTUS_TOKEN = directusToken;
