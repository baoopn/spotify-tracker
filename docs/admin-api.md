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

## Audit log

`GET /admin/api/audit?limit&cursor` → `{ data: [{ id, at, actor, action, detail, ip }], next_cursor }`.
`actor` is the admin's email (or `system`). Actions: `auth.forbidden`, `spotify.authorized`,
`spotify.authorize_failed`, `settings.email.updated`, `settings.email.tested`,
`enrichment.requeued`.

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
