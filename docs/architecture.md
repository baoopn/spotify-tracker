# Architecture

## Services and responsibilities

- **api** is the only component with database access. All business rules
  (history, replay detection, enrichment scheduling, token handling) live in
  `internal/tracker`, `internal/spotify.Manager` and `internal/store`.
- **poller** and **enricher** are stateless workers that talk to the API over
  gRPC (`api/proto/tracker/v1/tracker.proto`). They can be restarted or scaled
  without coordination.
- **PostgreSQL** is the source of truth. **Redis** holds derived and ephemeral
  state: caches, playback state for replay detection, the enrichment stream,
  Spotify access tokens, OAuth state, heartbeats and analytics counters.

## gRPC services (internal, port 9000)

| Service | Caller | Purpose |
|---|---|---|
| `IngestService.RecordPlayback / ClearPlayback` | poller | Persist a playback snapshot in one transaction. |
| `EnrichmentService.ListPendingTracks / SubmitTrackFeatures / ReportEnrichmentFailure` | enricher | Work queue backed by `track_features`. |
| `SpotifyService.GetAccessToken` | poller | Short-lived Spotify access tokens. The client secret and refresh token never leave the API. |
| `grpc.health.v1.Health` | probes | Standard health checking. |

Calls are authenticated with a shared bearer token (`INTERNAL_API_TOKEN`).
The port is never published: it lives on the private Docker network. Clients
retry `UNAVAILABLE` with exponential backoff.

## Data model

```
artists ─┐                 ┌─ albums
         └─ track_artists ─┤
                           tracks ──┬── listening_history   (track_id, played_at) unique
                                    ├── track_features      1:1, enrichment status + backoff
                                    └── current_playback    singleton row
settings (encrypted secrets) · admin_audit · api_request_stats (hourly aggregates)
top_playlists ── top_playlist_tracks   (one generated playlist per completed week/month/year)
```

Migrations are plain SQL in `migrations/` (goose, embedded in the binary,
applied under a Postgres advisory lock).

## Playback → history rules

Same semantics as the original Deno poller:

1. A different track than the last snapshot starts a new history row (`played_at` = Spotify's snapshot timestamp).
2. The same track whose progress jumps back by more than `REPLAY_RESET_THRESHOLD` (10 s) increments `replay_count`.
3. "Nothing playing" forgets the last track, so resuming later starts a new row.

The last state is kept in Redis, so restarts don't create duplicates, and the
`(track_id, played_at)` unique key makes ingestion and imports idempotent.

## Enrichment

When a new track is seen, a `pending` row is created in `track_features` and
the track is pushed to a Redis stream (consumer group `enricher`). The
enricher also sweeps pending rows every `ENRICH_INTERVAL_MS` as a safety net.
Lookups are rate limited client-side (`SOUNDCHARTS_RPS`, shared by
`ENRICH_CONCURRENCY` workers). A 429 pauses all workers for `Retry-After`.
Failures back off exponentially (1 min → 24 h) up to `ENRICH_MAX_ATTEMPTS`,
then the track is marked `failed`; it can be re-queued from the dashboard.
ISRCs unknown to Soundcharts are marked `unavailable`.

## Spotify tokens

- Authorization Code flow from the dashboard (Settings → Spotify). The
  callback is `PUBLIC_URL/admin/api/spotify/callback`, protected by a
  single-use `state` stored in Redis and tied to the admin who started it.
- The refresh token is stored AES-256-GCM encrypted (`SECRETS_KEY`) in
  `settings`. Rotated refresh tokens are persisted automatically.
- Access tokens are cached in Redis until a minute before expiry. On a 401 the
  poller asks for a forced refresh.
- Refresh tokens expire 180 days after authorization. The API emails the
  configured recipients 14/7/3/1 days before expiry, on expiry, and when
  Spotify answers `invalid_grant` (de-duplicated through Redis).

## Artist images

The API fills `artists.image_url` in the background (`internal/artistimages`)
from `GET /artists/{id}`. Every minute it looks up to 25 artists that have
never been checked (most recently played first), 2 s apart, then re-checks
each one every 30 days. A 429 pauses it for `Retry-After`. A refused lookup or
an artist with no image keeps any image stored earlier. Clients fall back to
the artist's initial when there is no image.

## Top 50 playlists

`internal/topplaylists` runs in the API (first tick a minute after start,
then every 10 minutes). For each enabled kind (`TOP_PLAYLISTS_PERIODS`) it
handles only the most recently completed period — ISO week, calendar month
or year, half-open `[start, end)` at local midnights in
`TOP_PLAYLISTS_TIME_ZONE` (DST-safe). Tracks are ranked by
`SUM(replay_count)` in that range (ties: most recently played). The row in
`top_playlists` is the state machine: `pending` → `created` (or `empty` when
nothing was played); `waiting_auth` while the stored scopes lack
`playlist-modify-public` (or Spotify answers 403); `failed` retries with
backoff (5 min doubling to 6 h, or Spotify's `Retry-After`). The Spotify
playlist ID is stored right after `POST /me/playlists`, so a retry only
re-runs the idempotent `PUT /playlists/{id}/items` and never creates a
duplicate. The ranked snapshot is kept in `top_playlist_tracks`. Invalid
settings are logged and disable the feature without stopping the API.

Kinds in `TOP_PLAYLISTS_LIVE` (default `week`) also get a **live** playlist of
the running period, handled by the same tick under the same advisory lock. On
the period's first plays the playlist is created with a "so far" description
(row `created`, `live = true`), ranking `[start, now)`. Every
`TOP_PLAYLISTS_LIVE_REFRESH` (default 1 h, min 10 min; `refreshed_at` is the
last success) it is re-ranked: an unchanged order writes nothing to Spotify
and only updates the snapshot's plays; a new order replaces the items first and
saves the snapshot after Spotify accepted them. Failures never unpublish: the
row keeps `created` / `live` / `published_at` and its old snapshot, and only
`last_error` and `next_attempt_at` (one interval later) change. A failed
initial creation follows the normal `failed` / `waiting_auth` retries.

Each tick first finalizes ended live rows in place (a store query for
`created AND live AND period_end <= now - 15 min`, not backing off, oldest
first, three per tick, so rows a long outage left behind are caught up too):
full-period ranking, items replaced if the order changed, `PUT /playlists/{id}`
with the final description, then `live = false`. Failures back off
(`attempts` / `next_attempt_at`) while the row stays live and published. The
15-minute grace lets late plays count; the admin's Regenerate finalizes
immediately. The next period's live playlist starts at its own start
regardless. The APIs report `live` only while the period is running. When a
final publish reuses an existing playlist (possibly created live), its
description is updated too; if that update fails, the row is published anyway
and left live (backing off) for the finalizer to retry. If saving a new
playlist ID fails after Spotify created it, the runner keeps the ID in memory
and the next attempt saves and reuses it instead of creating another. When
the create response itself was lost, the retry (`attempts > 0`, no stored ID)
first looks for an owned playlist with the exact name in `GET /me/playlists`
and adopts it.

After the generate and live steps each tick prunes, renames and checks, in
that order (a Spotify rate limit at any step ends the tick):

- **Prune.** Only the newest `TOP_PLAYLISTS_KEEP` (default 3; `0` keeps all)
  `created` rows per kind are kept, the live and hidden ones included. Older
  rows are pruned oldest first, ten per tick: the row becomes `pruned`
  (`pruned_at`, not live, snapshot deleted) before Spotify is asked to delete
  the playlist, so nothing restores it, and upserts never touch a pruned row.
  The removal (`DELETE /me/library?uris=spotify:playlist:{id}`, falling back
  to the deprecated `DELETE /playlists/{id}/followers` on 403 or 404; only
  that endpoint's 404 counts as already gone) sets `spotify_removed`; failures keep the row pruned with `last_error` and are
  retried each tick. Admin Generate refuses a period that would be pruned
  right away; regenerate and hide refuse pruned rows.
- **Rename.** Names are `{owner}'s Top 50 · {label}`. The owner is
  `TOP_PLAYLISTS_OWNER`, or else the connected account's display name
  (`GET /me`, cached 24 h, not read while Spotify is not connected; an expired
  name is kept when the read fails). With
  no owner at all new playlists get `Top 50 · {label}` and the rename step is
  skipped, so names never flap on a transient failure. Created rows whose
  stored name differs get `PUT /playlists/{id}` (five renamed per tick, at most
  ten Spotify writes, so failing rows cannot starve the rest). A re-followed
  playlist is renamed right after its restore.
- **Check and restore.** Every `TOP_PLAYLISTS_VERIFY` (default 1 h, min 10 min,
  `0`/`off` disables; the newest `verified_at` is the last check) the created
  rows are compared with the user's library (`GET /me/playlists`, every
  page). Rows published or restored in the last 15 minutes are skipped (a new
  playlist may not be listed yet). Nothing is changed when the list fails, is
  too long to read in full (over 1000 playlists), is empty while playlists
  are published, or contains none of the published playlists (likely
  another account; this holds for a single playlist too). A missing
  playlist (three per check) is followed again (`PUT /me/library`, falling
  back to the deprecated `PUT /playlists/{id}/followers`; the library endpoint
  has no visibility option, so `PUT /playlists/{id} {"public": true}` follows
  it) and its items reset
  to the stored snapshot; when Spotify no longer has it at all, a new playlist
  is created and its ID saved at once (same no-duplicate rule as above). The
  row stays `created` with `restored_at`; a failure only sets `last_error`. A
  404 from a live refresh, finalize, regenerate or rename runs the same
  restore; a failed restore there backs off like other live refresh or
  finalize failures. The admin's Check now forces a check.

## Admin dashboard

SvelteKit SPA built to static files and embedded with `go:embed`, served at
`/admin` with a strict CSP. Script hashes are computed from `index.html` at
startup, and the Clerk Frontend API host is derived from the publishable key.
Authentication is by Clerk: the SPA sends the Clerk session JWT as a Bearer
token, and the API verifies it against Clerk's JWKS (authorized party = our
origin) and checks an allow-list of emails / user IDs. Every sensitive action
is written to `admin_audit`.

## Brand assets

Logos, icons, favicons, og:image and the email logo come from the baoopn brand
API (`BRAND_API_URL`, default `https://baoopn.com/api/v1/brand/listening`), so a
file replaced in the baoopn admin shows up here within minutes, without a
deploy. `internal/brandkit` fetches the JSON every `BRAND_REFRESH` (default 60
s, 3 s timeout) and keeps the last good copy in memory; a failed refresh is
logged and never clears it. An entry is used only when its URL is https on
`media.baoopn.com` or `baoopn.com` (a relative `/media/...` URL is resolved
against the API origin) and its content type fits the slot (svg, png or ico).

- `GET /brand/{slot}` redirects (302) to the cached media URL, or to the static
  copy in `web/public/static` when nothing is cached. The frontends use these
  stable paths, so the prerendered HTML and its CSP hashes never change.
- `/favicon.ico`, `/favicon.svg`, `/apple-touch-icon.png`, `/favicon-{16,32,48}.png`,
  `/icon-{192,512}.png`, `/icon-maskable-512.png` and `/og-image.png` redirect
  to the media URL when cached, else serve the embedded static file.
- `/site.webmanifest` and `/admin/site.webmanifest` are generated; their icons
  point at the media URLs (static paths as the fallback).
- Every brand response has `Cache-Control: public, max-age=300`.
- An empty `BRAND_API_URL` disables the API; the static copies are then always
  used. Every slot has a static copy (checked by a test).

## Observability

- Structured JSON logs (`log/slog`) with request IDs.
- Prometheus metrics on `OPS_ADDR` (`/metrics`), plus `/healthz` and
  `/readyz` (dependency checks) on every service. The containers' healthcheck
  is `<binary> healthcheck`, so distroless images need no curl.
- Workers publish heartbeats to Redis, which the dashboard shows as service health.
- API analytics (public `/api/v1` traffic only) are aggregated hourly in Postgres:
  counts, status codes and a latency histogram for p50/p95/p99. Unique
  clients are counted per day with a Redis HyperLogLog of salted IP hashes,
  so no raw IPs are stored. Data is pruned after 90 days.

## Security notes

- Secrets are read from the environment and unset after loading (`unset` tag).
- Containers run as non-root on distroless images (read-only root filesystem
  and no capabilities in the VPS compose). Postgres, Redis and gRPC are never
  exposed.
- The client IP trust model is explicit (`TRUSTED_PROXY_HOPS` /
  `CLIENT_IP_HEADER`), so `X-Forwarded-For` can't be spoofed into the rate limiter.
- `GET /api/v1/now-playing/stream` (SSE) holds one Redis subscription per
  stream, so streams are capped: at most 6 per client IP (429) and 256 in all
  (503); each stream closes after about 30 minutes and clients reconnect.
