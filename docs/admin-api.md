# Admin API contract

The admin dashboard is a SvelteKit SPA served by the API at **`/admin`** (same
origin). Its backend lives under **`/admin/api`**. All bodies are JSON
(`Content-Type: application/json`); timestamps are RFC 3339 UTC strings.

## Conventions

### Authentication (Clerk)

- Sign-in is handled by **Clerk** in the SPA (`@clerk/clerk-js`, or a
  maintained Svelte 5 wrapper such as `svelte-clerk`). The publishable key is
  NOT baked into the build: the SPA fetches it at startup from the public
  endpoint `GET /admin/api/config` → `{ "clerk_publishable_key": "pk_…", "version": "…" }`.
- Every `/admin/api` request (except `/config` and `/spotify/callback`) sends
  **`Authorization: Bearer <token>`** where the token is
  `await clerk.session.getToken()` (fetch a fresh one per request; Clerk caches
  it). No cookies or CSRF tokens are used by the admin API.
- The API verifies the JWT (Clerk JWKS) and then checks an allow-list of
  Clerk user IDs / emails configured on the server.
  - Missing/invalid token → `401 {"error":{"code":"unauthorized",…}}` → SPA shows Clerk sign-in.
  - Valid Clerk user but not allowed → `403 {"error":{"code":"forbidden",…}}` →
    SPA shows an "access denied" screen with a sign-out button.

### Errors

```json
{ "error": { "code": "invalid_request", "message": "human readable", "fields": { "port": "must be 1..65535" } } }
```
`fields` is optional (validation errors, HTTP 422 `validation_failed`).

### Lists

```json
{ "data": [ … ], "total": 1234, "next_cursor": "…" }
```
`total` for offset lists, `next_cursor` for cursor lists.

### Shared shapes

```ts
type Artist = { id: string; name: string; uri?: string; external_url?: string;
                image_url?: string }  // profile image (i.scdn.co), filled in the background
type Album  = { id: string; name: string; release_date?: string; cover_image_url?: string; uri?: string; external_url?: string }
type TrackView = { id: string; name: string; uri?: string; isrc?: string; duration_ms: number; explicit: boolean;
                   external_url?: string; album?: Album; artists: Artist[] }
type NowPlaying = { is_playing: boolean; progress_ms: number | null; fetched_at: string; track: TrackView | null }
type EnrichmentStatus = "pending" | "enriched" | "unavailable" | "failed"
type AudioFeatures = { acousticness, danceability, energy, instrumentalness, liveness, loudness,
                       speechiness, tempo, valence: number | null; key, mode, time_signature: number | null }
type Genre = { root: string; sub: string[] }
```

---

## Auth

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/admin/api/config` | – (public) | `{ "clerk_publishable_key": "pk_…", "version": "…" }` |
| GET | `/admin/api/auth/me` | – | `200 { "user": { "id", "email", "name", "image_url" } }`, `401`, or `403` |

Sign-out is done client-side with Clerk (`clerk.signOut()`).

## Overview

`GET /admin/api/overview`

```json
{
  "now_playing": NowPlaying | null,
  "summary_all": Summary, "summary_7d": Summary,
  "enrichment": { "pending": 0, "enriched": 0, "unavailable": 0, "failed": 0 },
  "spotify": SpotifyStatus,
  "services": [
    { "name": "api",      "status": "ok" | "degraded" | "down", "last_seen_at": "…", "detail": "…" },
    { "name": "poller",   "status": "…", "last_seen_at": "…", "detail": "last poll ok / error text" },
    { "name": "enricher", "status": "…", "last_seen_at": "…", "detail": "…" },
    { "name": "postgres", "status": "…" }, { "name": "redis", "status": "…" }
  ],
  "version": "1.2.3"
}
```
`Summary = { total_plays, unique_tracks, unique_artists, listening_ms, first_played_at, last_played_at, enriched_tracks }`.

## Spotify

```ts
type SpotifyStatus = {
  connected: boolean            // a refresh token is stored
  source: "env" | "dashboard"   // where the current token came from
  authorized_at: string | null
  expires_at: string | null     // authorized_at + lifetime_days
  days_left: number | null
  lifetime_days: number         // 180
  scopes: string[]
  redirect_uri: string          // must be registered in the Spotify dashboard
  client_id: string             // public identifier, shown for convenience
  last_refresh_at: string | null
  last_error: string | null     // e.g. invalid_grant
  account: { id: string; display_name: string } | null
}
```

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/admin/api/spotify` | – | `SpotifyStatus` |
| POST | `/admin/api/spotify/authorize` | – | `{ "url": "https://accounts.spotify.com/authorize?..." }` — SPA sets `window.location.href = url`. |
| GET | `/admin/api/spotify/callback` | (Spotify redirect) | 302 → `/admin/settings/spotify?result=success` or `?result=error&message=…` |
| POST | `/admin/api/spotify/test` | – | `{ "ok": true, "status": 200 \| 204, "now_playing": NowPlaying \| null }` or `{ "ok": false, "error": "…" }` |

## Email / SMTP settings

```ts
type EmailSettings = {
  host: string; port: number; username: string; from: string
  alert_to: string[]
  password_set: boolean
  enabled: boolean          // host, from and alert_to are all set
  expiry_alert_days: number[]  // e.g. [14, 7, 3, 1]
  updated_at: string | null
}
```

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/admin/api/settings/email` | – | `EmailSettings` |
| PUT | `/admin/api/settings/email` | `{ host, port, username, from, alert_to: string[], expiry_alert_days: number[], password?: string }` — omit `password` to keep, `""` to clear | `EmailSettings`; `422` with `fields` |
| POST | `/admin/api/settings/email/test` | `{ "to"?: string }` (defaults to alert_to) | `{ "ok": true }` or `{ "ok": false, "error": "…" }` |

## Data

| Method | Path | Query | Response |
|---|---|---|---|
| GET | `/admin/api/history` | `limit` (1–100, default 50), `cursor`, `q` (track/artist/album search) | `{ data: HistoryEntry[], next_cursor }`, `HistoryEntry = { id, played_at, updated_at, replay_count, track: TrackView }` |
| GET | `/admin/api/tracks` | `q`, `status` (EnrichmentStatus), `sort` (`plays`\|`last_played`\|`name`\|`created`), `order` (`asc`\|`desc`), `limit`, `offset` | `{ data: TrackRow[], total }`, `TrackRow = { track: TrackView, plays, last_played: string\|null, enrichment_status: EnrichmentStatus, created_at }` |
| GET | `/admin/api/tracks/{id}` | – | `TrackAdminDetail = TrackView & { plays, last_played, created_at, enrichment: { status, source, attempts, last_error, next_attempt_at, enriched_at, features: AudioFeatures, genres: Genre[] } \| null }` |
| POST | `/admin/api/tracks/{id}/enrich` | – | `202 { "queued": true }` (resets to pending and queues) |
| POST | `/admin/api/enrichment/retry-failed` | – | `{ "requeued": 12 }` |
| GET | `/admin/api/artists` | `q`, `sort` (`plays`\|`name`\|`tracks`), `order`, `limit`, `offset` | `{ data: [{ artist: Artist, plays, tracks, last_played }], total }` |

The public read endpoints (`/api/v1/stats/*`, `/api/v1/now-playing/stream`) can be
used by the dashboard too (same origin), e.g. for top tracks/genres charts.

## Analytics (public API traffic)

`GET /admin/api/analytics?range=24h|7d|30d` (default `24h`). Admin, health and
metrics routes are excluded from analytics.

```json
{
  "range": "24h",
  "bucket": "hour",
  "totals": { "requests": 0, "errors": 0, "error_rate": 0.0, "p50_ms": 0, "p95_ms": 0, "p99_ms": 0, "unique_clients": 0 },
  "timeseries": [ { "bucket": "2026-11-05T17:00:00Z", "requests": 0, "errors": 0, "p95_ms": 0 } ],
  "routes": [ { "route": "/api/v1/now-playing", "method": "GET", "requests": 0, "errors": 0, "p50_ms": 0, "p95_ms": 0, "avg_ms": 0 } ],
  "status_codes": [ { "code": 200, "count": 0 } ],
  "top_origins": [ { "origin": "https://example.com", "count": 0 } ]
}
```
`errors` = status ≥ 500. Buckets are hourly (`24h`, `7d`) or daily (`30d`);
empty buckets are included with zeros. `timeseries` is ordered oldest first.

## Analytics (public page visitors)

`GET /admin/api/analytics/pages?range=24h|7d|30d&tz=<IANA>` (same ranges and
time-zone handling as the API analytics):

```json
{
  "range": "24h", "bucket": "hour", "time_zone": "UTC",
  "totals": { "views": 0, "visitors": 0, "avg_visible_ms": 0, "live_now": 0 },
  "timeseries": [ { "bucket": "2026-11-05T17:00:00Z", "views": 0 } ],
  "paths": [ { "name": "/", "views": 0 } ],
  "referrers": [ { "name": "google.com", "views": 0 } ],
  "devices": [ { "name": "mobile", "views": 0 } ],
  "browsers": [ { "name": "Safari", "views": 0 } ],
  "countries": [ { "name": "CA", "views": 0 } ]
}
```

- **Collection**: the public page sends `POST /api/v1/events` beacons, one
  `{"type":"pageview","path","referrer","width"}` per page view and
  `{"type":"engagement","path","width","visible_ms"}` when the page is hidden.
  The endpoint always answers `202` for a valid event, or `400`/`413` otherwise.
- **Privacy**: no cookies, no client storage, no IDs; IPs are never stored.
  `visitors` comes from a salted hash of IP + user agent that changes every UTC
  day, kept in a Redis HyperLogLog for 40 days. `live_now` counts visitors seen
  in the last 5 minutes. Rows keep only coarse dimensions: device class from
  the viewport width, browser family, Cloudflare's country code, and the
  referring host (empty = direct, including visits from the site itself).
- **Exclusions**: automated browsers (`navigator.webdriver`, which covers the
  e2e tests) don't send beacons; bots, crawlers, monitors and headless
  browsers matched by user agent are accepted but not counted.
- `avg_visible_ms` = total visible time ÷ views; each report is capped at 30
  minutes. Data is pruned after 90 days, like the API analytics.

## Audit log

`GET /admin/api/audit?limit&cursor` → `{ data: [{ id, at, actor, action, detail, ip }], next_cursor }`.
`actor` is the admin's email (or `system`). Actions: `auth.forbidden`, `spotify.authorized`,
`spotify.authorize_failed`, `settings.email.updated`, `settings.email.tested`,
`enrichment.requeued`, `playlist.refreshed`, `top_playlists.run`,
`top_playlists.generated`, `top_playlists.regenerated`, `top_playlists.hidden`,
`top_playlists.shown`.

## Playlists

Plays record their Spotify *context* (`listening_history.context_type/context_id`).
Playlist metadata is fetched from Spotify when a playlist is first played from
(refreshed after 24 h). Spotify-generated mixes and playlists the app can't
read are stored with `available: false` (no name/cover).

```ts
type Playlist = {
  id: string; available: boolean; name?: string; description?: string;
  owner_id?: string; owner_name?: string; cover_image_url?: string;
  external_url?: string; uri?: string; public: boolean | null;
  collaborative: boolean; tracks_total: number | null; fetched_at: string
}
type RecentPlaylist = { playlist: Playlist; plays: number; last_played: string }
```

| Method | Path | Query | Response |
|---|---|---|---|
| GET | `/admin/api/playlists` | `limit` (1–100), `offset` | `{ data: RecentPlaylist[], total }` — every playlist ever played from, newest first, incl. private/unavailable |
| POST | `/admin/api/playlists/{id}/refresh` | – | `Playlist` (re-fetched from Spotify) |

Public (no auth): `GET /api/v1/playlists/recent?limit=5` → `{ data: RecentPlaylist[] }`
— only public playlists (unless `PUBLIC_SHOW_PRIVATE_PLAYLISTS=true`), cached.

## Top 50 playlists

The API generates a public Spotify playlist of the 50 most-played tracks of
every completed week (ISO, Monday–Sunday), month and year
(`TOP_PLAYLISTS_PERIODS`, boundaries in `TOP_PLAYLISTS_TIME_ZONE`). Each
period has one row whose `status` is its state: `pending` → `created` |
`empty`, with `waiting_auth` (Spotify not connected or missing
`playlist-modify-public`; re-authorize in Settings → Spotify) and `failed`
(retried with backoff, see `next_attempt_at`). A generator tick runs every
`interval_seconds` and handles the most recently completed period of every
enabled kind; the endpoints below run it on demand, backfill older periods and
regenerate existing ones.

```ts
type AdminTopPlaylist = {
  period: 'week' | 'month' | 'year';
  period_start: string;        // RFC 3339 in the row's zone, e.g. "2026-11-02T00:00:00-05:00"
  period_end: string;          // exclusive
  time_zone: string;           // zone the period was generated in
  label: string;               // "Week of Nov 2, 2026", "November 2026", "2026"
  name: string;                // Spotify playlist name
  status: 'pending' | 'created' | 'empty' | 'waiting_auth' | 'failed';
  hidden: boolean;             // left out of the public page
  spotify_id: string | null;
  spotify_url: string | null;
  track_count: number;
  total_plays: number;
  attempts: number;            // failed attempts since the last success
  last_error: string | null;
  next_attempt_at: string | null;  // UTC; earliest automatic retry
  published_at: string | null;     // UTC; last successful publish
  created_at: string;
  updated_at: string;
};
type TopPlaylistTrack = { position: number; plays: number; track: TrackView };  // as in /api/v1/playlists/top
type TopPlaylistsStatus = {
  enabled: boolean;
  periods: ('week' | 'month' | 'year')[];  // enabled kinds, [] when disabled
  time_zone: string;                       // configured zone ("UTC" when disabled)
  authorized: boolean;                     // Spotify connected and playlist-modify-public granted (unknown scopes count as granted)
  interval_seconds: number;                // 600
};
```

`{period}` is `week`, `month` or `year`; `{start}` is a row's `period_start`
exactly as returned, URL-encoded (`encodeURIComponent`). Any RFC 3339 form of
the same instant is accepted.

| Method | Path | Body / query | Response |
|---|---|---|---|
| GET | `/admin/api/top-playlists` | `period` (optional), `limit` (1–100, default 50), `offset` | `{ data: AdminTopPlaylist[], total, status: TopPlaylistsStatus }` — every row of every status, newest period first |
| GET | `/admin/api/top-playlists/{period}/{start}` | – | `{ data: AdminTopPlaylist & { tracks: TopPlaylistTrack[] } }` — with the stored snapshot (`[]` when none) |
| POST | `/admin/api/top-playlists/run` | – | `{ data, total, status }` (first page, after the run) — runs one generator tick now |
| POST | `/admin/api/top-playlists/generate` | `{ "period": "month", "date": "2026-07-15" }` | `{ data: AdminTopPlaylist }` — generates (or refreshes) the completed period containing `date` (a calendar date in the configured zone) |
| POST | `/admin/api/top-playlists/{period}/{start}/regenerate` | – | `{ data: AdminTopPlaylist }` — regenerate / retry an existing row |
| PATCH | `/admin/api/top-playlists/{period}/{start}` | `{ "hidden": true }` | `{ data: AdminTopPlaylist }` — hide or show it on the public page |

Generate and regenerate are forced: they ignore the created / empty / backoff
rules of the tick, re-rank the period, replace the stored snapshot and then
create the Spotify playlist if the row has none, or replace the items of the
**same** playlist (never a duplicate). An `empty` period whose plays were
imported later becomes `created`. Mutation responses reflect the stored row;
failures are stored on the row exactly like the tick stores them. Hiding
drops the public response cache; the playlist stays in the Spotify account.
There is no delete.

Errors: `400 invalid_period | invalid_start | invalid_date | invalid_body |
invalid_limit | invalid_offset`, `400 period_not_completed` (the period is
still running or in the future), `400 invalid_period` also when the kind is
not in `TOP_PLAYLISTS_PERIODS`, `404 not_found`, `409 busy` (another
generation holds the lock; retry shortly), `412 spotify_not_authorized` (the
row is now `waiting_auth`; `message` is its `last_error`), `502 spotify_error`
(the row is now `failed`; `message` includes its `last_error`), `503
top_playlists_disabled` for every mutation when the feature is disabled (the
list still answers, with `status.enabled: false`). Run, generate and
regenerate are bounded to 25 s.

Audit actions: `top_playlists.run`, `top_playlists.generated`,
`top_playlists.regenerated`, `top_playlists.hidden`, `top_playlists.shown`
(detail: `period`, `period_start`, `status`, `spotify_id`; `run`: `periods`).

## Time handling

- All timestamps in requests and responses are RFC 3339 **UTC** instants;
  the server stores `timestamptz` and computes in UTC.
- `GET /admin/api/analytics` accepts `tz` (IANA name, e.g. `Asia/Ho_Chi_Minh`;
  default UTC). Daily buckets (`30d`) are calendar days in that zone,
  DST-safe; the response echoes `time_zone`. Hourly buckets are UTC hours.
  Unique-client counts are per UTC day.
- Clients must format with `Intl.DateTimeFormat` using the browser's zone
  (`Intl.DateTimeFormat().resolvedOptions().timeZone`) and never hand-roll UTC
  offsets. Date-only strings such as album `release_date` ("2020",
  "2020-05", "2020-05-01") are calendar values, not instants — never parse
  them with `new Date()`.

### Stale browser time-zone data

Browsers bundle their own copy of the IANA database (ICU), which can lag
real-world rule changes (e.g. a region abolishing or adopting DST).
`GET /api/v1/timezone?tz=<IANA>` (public, cached 5 min) returns the server's
current view: `{ tz, abbreviation, offset_minutes, offset: "-06:00", now, source }`.
Clients compare it with `Intl` for the same zone; if they disagree, they format
with `timeZone: offset` (fixed-offset `Intl` zone) and label it `GMT-6`. A user
override (Appearance → Time zone) covers browsers that can't report a zone.
