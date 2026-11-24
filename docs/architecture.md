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
tracks.track_number / disc_number · albums.album_type / total_tracks / release_date_precision
albums.upc / copyrights / details_fetched_at · artists.genres / genres_fetched_at
top_playlists ── top_playlist_tracks   (one generated playlist per completed week/month/year)
```

Migrations are plain SQL in `migrations/` (goose, embedded in the binary,
applied under a Postgres advisory lock).

The Spotify metadata columns are all nullable. The poller fills track and
disc numbers, album type, total tracks and release date precision from
`currently-playing`; an empty value never overwrites a stored one. The
background jobs below fill the rest. `artists.genres` is NULL when unknown and
`'{}'` when Spotify lists none. `albums.copyrights` is NULL until the album is
looked up.

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
Rate limits (429, or 503 with `Retry-After`) are reported as `retryable` and
only push `next_attempt_at` back (`Retry-After` when sent, else the backoff
for the attempts so far) without using up an attempt, for at most 24 hours
from the first one (`postponed_since`); after that they count again. Other
5xx, network and body errors always count.
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

## Artist images and genres

The API fills `artists.image_url` and `artists.genres` in the background
(`internal/artistimages`) from `GET /artists/{id}`. Every minute it looks up
to 25 artists, 2 s apart, and re-checks each one every 30 days. Artists that
were never checked come first, then artists whose genres were never looked up
(`genres_fetched_at` is NULL), so every artist stored before genres existed is
looked up once more. Within each group, and among stale artists, the most
recently played come first, then the oldest lookup. A 429 pauses it for
`Retry-After`. A 5xx or network error skips that artist until the next batch;
three such errors in a row end the batch. A refused lookup or an artist with
no image keeps any image stored earlier, and a refused lookup keeps the stored
genres. Genres are
trimmed and de-duplicated, at most 20 per artist. Clients fall back to the
artist's initial when there is no image. Genres are shown only on track
detail endpoints.

## Album details

`internal/albumdetails` fills in album details from `GET /albums/{id}`:
`album_type`, `total_tracks`, `release_date_precision`, the UPC
(`external_ids.upc`), the copyrights (at most 10, each at most 500
characters) and the track and disc numbers of the album's tracks. Every
minute it looks up to 15 albums, 2 s apart: albums never checked first (most
recently played first), then the oldest lookups. Each album is re-checked
every 180 days. Album fields and track numbers are written in one statement,
and a missing value never overwrites a stored one. Only the first page of the
track list is read (up to 50 tracks); tracks past it get their numbers from
the poller. Relinked tracks are matched through `linked_from`.

A 403, 404 or other 4xx only records the lookup time (`details_fetched_at`)
and leaves `copyrights` NULL, so the album is tried again after 7 days. A 403
is logged at warn and a 404 at debug, so a refusal of every album stands out
in the logs. A 429 pauses the job for `Retry-After`. A 5xx, a network or
decode error, or a 401 that persists after one token refresh counts as
transient: the album is retried in about an hour (its lookup time is set to
`now - 180 days + 1 hour`) and the batch goes on with the next album; three
transient errors in a row end the batch. Nothing is looked up while Spotify
is not connected.

## Lyrics

`internal/lyrics` looks up track lyrics on [LRCLIB](https://lrclib.net)
(`internal/lrclib`, no API key) and stores them in the `lyrics` table
(migration `00014_lyrics.sql`). Lyrics are admin and MCP only, with one
public exception: the [lyrics of the song playing now](#public-now-playing-lyrics).
No other `/api/v1` route, no public MCP tool and none of the shared track JSON
types carry them (guard tests in `internal/httpapi` and `internal/mcpserver`).

- **Schedule.** Every `LYRICS_INTERVAL` (1 min) it looks up `LYRICS_BATCH`
  (10) tracks: tracks with no lyrics row first (most recently played first),
  then due retries. The client sends at most one request per
  `LYRICS_SPACING` (2 s) and identifies itself with the User-Agent
  `spotify-tracker (https://github.com/baoopn/spotify-tracker)`.
  `LYRICS_ENABLED=false` makes no LRCLIB calls.
- **Matching.** `GET /api/get` with the track name, first artist, album and
  duration (rounded seconds). On a 404 (or an empty record) it falls back to
  `GET /api/search`. A search candidate must have the same folded track name
  (a version suffix such as "(Live)" or "- Remastered" is ignored), an artist
  name that contains the first artist or is contained in it, and, when the
  duration is known, a duration within 2 s. Among those it prefers the exact
  name, then synced lyrics, then plain lyrics, then the closest duration.
  Without an album or duration only search is used. NUL bytes are removed,
  line breaks normalized to `\n`, and each text is capped at 200 000 bytes.
  When only synced lyrics exist, plain text is derived by removing the
  timestamps; a record with no lyrics text left is `instrumental`.
- **States.** `found` and `instrumental` are final (never overwritten by a
  later failure). `not_found` is retried after `LYRICS_NOT_FOUND_RETRY`
  (30 days). A transient error (5xx, 408, timeout, network, decode) or a
  failed database write is retried after 1 h, doubling per error in a row up
  to 7 days (`attempts` restarts at 1 when the status changes, so monthly
  not-found checks do not grow the error backoff). Three transient errors in
  a row end the batch, and the next batch waits twice the interval, doubling
  per such batch up to 1 h; a batch with a successful lookup resets it. A
  permanent 4xx is retried after 7 days and does not end the batch. A 429
  writes nothing and pauses the job for `Retry-After` (seconds or an HTTP
  date; default 1 min, at most 1 h). A track without a row is reported as
  `pending`.
- **Search.** `search_tsv` is a stored generated column,
  `to_tsvector('simple', search_fold(plain_lyrics))`, with a GIN index. The
  `simple` configuration fits Vietnamese (space-separated syllables, no
  stemming or stop words) and `search_fold` (migration 00010) folds accents
  without an extension. Queries use `websearch_to_tsquery`; exact phrases
  (`phraseto_tsquery`) rank first. A search is cut off after 5 s. A later change to `search_fold` must
  recompute the column (`UPDATE lyrics SET plain_lyrics = plain_lyrics`).

### Public now-playing lyrics

`GET /api/v1/now-playing/lyrics` (`internal/httpapi/now_lyrics.go`) serves the
public page the full lyrics of the song playing now, and of no other song:

- **Current track only.** It takes no parameters (a query string is ignored)
  and reads the now-playing snapshot. It answers `{"data":null}` when nothing
  plays, playback is paused, the snapshot is older than 60 s (poller down),
  the estimated position is past the track's end, or the track has no lyrics
  (yet).
- **Full lyrics.** `store.NowLyrics` reads the track's found or instrumental
  lyrics. Synced LRC text is parsed by `internal/lrc` (several time tags per
  line, `[offset:]`, metadata and untimed lines dropped) and sent as
  `lines: [{ start_ms, text }]` with `synced: true` (at most 2000 lines of at
  most 200 runes each). Without synced lines the plain text is sent as
  `plain` (at most 20 000 runes). Instrumental tracks get
  `instrumental: true` and no text. The page highlights the current line
  from its own position estimate.
- **Credit and caching.** Every answer carries `credit` ("Lyrics from
  LRCLIB", <https://lrclib.net>). `Cache-Control: no-store`, so Cloudflare
  never caches it. The current track's answer is kept in memory for 1 min
  (10 s when there are no lyrics yet), and concurrent misses share one store
  read, so the database is read at most about once a minute per replica.
- **Not in MCP.** The route is registered next to `v1Routes`, not in it, so
  `V1Handler` (and with it the MCP server) cannot reach it.

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
the create response itself was lost (or the process died mid-create), the
next attempt on a row that already existed without a stored ID first looks
for an owned playlist with the exact name in `GET /me/playlists` and adopts
it; a final attempt then sets the final description on it.

After the generate and live steps each tick prunes, renames and checks, in
that order (a Spotify rate limit at any step ends the tick):

- **Prune.** Only the newest `TOP_PLAYLISTS_KEEP` (default 3; `0` keeps all)
  `created` rows per kind are kept, the live and hidden ones included. Older
  rows are pruned oldest first, ten per tick, and so are `failed` and
  `waiting_auth` rows that hold a Spotify playlist and have `TOP_PLAYLISTS_KEEP`
  newer `created` rows of their kind (they are no longer generated): the row becomes `pruned`
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
  another account; this holds for a single playlist too, so a sole published
  playlist deleted by hand is not restored). A missing
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

## MCP server

`cmd/api` also serves a remote [MCP](https://modelcontextprotocol.io) server at
`/mcp` (`internal/mcpserver`, official Go SDK), so AI clients such as Claude can
read the listening data. It uses the Streamable HTTP transport in **stateless**
mode with plain JSON responses: no server-side sessions and no SSE streams, so
it works behind Cloudflare and on several replicas. Only `POST /mcp` is used
(GET and DELETE answer 405). `MCP_ENABLED=false` removes the route (404).

- **Auth.** No `Authorization` header (or `Bearer` with an empty token) gets the
  public tools. A valid admin token adds the admin tools. Two tokens are valid:
  the **dashboard token** (generated under Settings → MCP; only its hex SHA-256
  is stored in the `mcp` setting, and the hashes are compared in constant
  time) and the optional **env token** `MCP_ADMIN_TOKEN` (bootstrap/fallback,
  at least 32 characters, constant-time compare). Any other token is rejected
  with 401 and `WWW-Authenticate: Bearer`; a wrong token is never downgraded to
  anonymous. Failed attempts are logged at warn (no token).
- **Dashboard token flow.** `POST /admin/api/mcp/token` creates (or rotates)
  a token `stm_` + 32 random bytes (base64url) and returns it once; the page
  shows it once with copy buttons and ready-made `claude mcp add` / `.mcp.json`
  snippets. Rotating or revoking (`DELETE /admin/api/mcp/token`) makes the old
  token fail with 401. The **Admin tools** toggle (`PUT /admin/api/mcp`) turns
  the admin tools off: a valid token then gets the public tools, a wrong one is
  still 401 (one exception for the env token, see below). `mcpserver` reads
  the setting through a 30 s cache that the admin API drops on every save, so
  changes apply at once on this replica and within 30 s on others. Concurrent
  requests share one load, and after a failed load the cache waits 5 s before
  it asks Postgres again. If a reload fails after the TTL ran out, the last
  loaded value is used. If there is no usable value (cold start with Postgres
  down, or a failed reload right after a save or revoke, so a revoked token
  never keeps working), the settings count as unavailable: the dashboard token
  and any other non-env token get 503, and the env token still works with the
  built-in defaults, which means **admin tools on even if the dashboard toggle
  is off**. Leave `MCP_ADMIN_TOKEN` empty if the toggle must hold during a
  database outage. The token's last use is written at most once per 5
  minutes, in the background.
  The settings and token routes are not in `admin.ReadOnlyHandler`, so an MCP
  client can never mint its own token.
- **Limits.** Own per-IP rate limit (`MCP_RATE_LIMIT_PER_MINUTE`, default 60),
  1 MiB request body, 30 s timeout. `/mcp` is not behind the `/api/v1`
  middleware, so MCP calls are not counted in the API analytics. The global
  CORS middleware still runs on `/mcp`, but it allows only `GET`, `HEAD` and
  `OPTIONS` with the `Accept` and `Content-Type` headers. A browser's
  preflight for `POST /mcp` (or for an `Authorization` header) is therefore
  refused, so MCP clients that run in a browser cannot use the endpoint.
  Desktop, CLI and server-side clients don't use CORS and are not affected.
- **Dispatch.** Each tool builds a `GET` request and runs it in-process through
  the existing handlers, so there is no second copy of the queries. Public
  tools go through `httpapi.Handler.V1Handler` (the `/api/v1` routes at the same
  paths, sharing the Redis cache, without rate limit, analytics, SSE stream or
  `POST /events`); admin tools through `admin.ReadOnlyHandler` (GET routes only,
  no Clerk middleware; never mounted publicly). Arguments are declared in each
  tool's input schema (enums, bounds, defaults) and still validated by the API.
  A 2xx answer is returned as JSON text plus structured content; a 4xx as a
  tool error with the API's message; anything else as "internal error". Path
  arguments are escaped as one segment, so they can't leave the route.
- **Public tools** (`readOnlyHint`, `openWorldHint: false`): `now_playing`,
  `recent_plays` (limit, cursor), `track_details` (id), `listening_summary`
  (range), `top_tracks` / `top_artists` / `top_genres` (range, limit),
  `audio_profile` (range), `recent_playlists` (limit), `top_playlists` (limit).
  None returns lyrics; the public now-playing lyrics are not reachable from MCP.
- **Admin tools** (read-only): `admin_overview`, `admin_spotify_status`,
  `admin_history` (limit, cursor, q), `admin_tracks` (q, status, sort, order,
  limit, offset), `admin_track` (id), `admin_track_lyrics` (id, synced),
  `admin_search_lyrics` (q, limit), `admin_artists` (q, sort, order, limit,
  offset), `admin_playlists` (limit, offset), `admin_top_playlists` (period,
  limit, offset), `admin_top_playlist` (period, start), `admin_backups`,
  `admin_api_analytics` / `admin_page_analytics` (range, tz). Write routes, the
  SMTP settings, the MCP settings, `/auth/me`, `/audit` and `/config` are not
  exposed. `mcpserver.PublicTools()` / `AdminTools()` list the names and
  descriptions for the dashboard.

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
- Lyrics reach the public only through `GET /api/v1/now-playing/lyrics`: the
  track playing now, a window of at most 10 synced lines, never plain text,
  `no-store`; it is outside `v1Routes`, so MCP cannot call it.
