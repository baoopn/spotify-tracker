# spotify-tracker

Tracks what you listen to on Spotify, enriches every track with audio features
and genres from [Soundcharts](https://soundcharts.com), and serves it through a
public page and read-only API plus a private admin dashboard, all on one domain
(production: <https://listening.baoopn.com>):

| Path | What |
|---|---|
| `/` | Public page: what's playing now + recently played |
| `/api/v1/*` | Public read-only REST API (+ SSE stream) |
| `/admin` · `/admin/api/*` | Admin dashboard (Clerk) and its API |

```
            ┌────────────┐  gRPC   ┌───────────────────────────────┐   SQL   ┌────────────┐
 Spotify ◄──│  poller    │────────►│              api              │────────►│ PostgreSQL │
            └────────────┘         │  REST /api/v1  ·  admin /admin    │         └────────────┘
            ┌────────────┐  gRPC   │  gRPC :9000 (internal only)   │  cache  ┌────────────┐
Soundcharts◄│  enricher  │────────►│                               │────────►│   Redis    │
            └─────┬──────┘         └───────────────────────────────┘         └─────┬──────┘
                  └──────────────── enrichment stream (Redis) ─────────────────────┘
```

| Component | What it does |
|---|---|
| `cmd/api` | Owns the database. Public REST API (`/api/v1`), live SSE stream, admin dashboard (`/admin`, Clerk auth), internal gRPC (ingest, enrichment, Spotify access tokens), alert emails, API analytics. |
| `cmd/poller` | Polls Spotify *currently playing* every 3 s and forwards snapshots over gRPC. While a song plays it also reads the queue, so the API can prefetch the next track (catalog, lyrics, translation, cache) before it starts. Holds no Spotify secrets: access tokens come from the API. |
| `cmd/enricher` | Consumes new tracks from a Redis stream (plus a periodic sweep with retry/backoff), fetches audio features + genres by ISRC from Soundcharts. |
| `cmd/migrate` | Applies database migrations (the API also does this on start). |
| `cmd/importer` | One-off, idempotent import of the legacy Directus data. |
| `web/admin` | SvelteKit (Svelte 5) admin SPA at `/admin`, embedded into the API binary. |
| `web/public` | Public now-playing page at `/` (SvelteKit, prerendered), embedded into the API binary. |

**Stack:** Go 1.27 · gRPC/protobuf (buf) · PostgreSQL 17 (pgx, goose) · Redis 7 ·
chi · Prometheus metrics · slog JSON logs · SvelteKit + Tailwind · Clerk ·
distroless Docker images · GitHub Actions → GHCR → Coolify.

## Quick start (local)

Prerequisites: Docker, Go 1.27+, Node 22 + pnpm (only to work on the frontends),
a Spotify **Premium** account with a Development Mode app
([setup](docs/spotify-token.md)), and Soundcharts API credentials.

```bash
cp .env.example .env      # fill in Spotify, Soundcharts, SECRETS_KEY, …
docker compose up --build # postgres, redis, api, poller, enricher
```

- Public page: <http://127.0.0.1:8080/> (now playing + recently played)
- Public API: <http://127.0.0.1:8080/api/v1/now-playing>
- Admin dashboard: <http://127.0.0.1:8080/admin/> (needs Clerk keys, see [docs/admin.md](docs/admin.md))
- Metrics: <http://127.0.0.1:9090/metrics>

Working on the code instead of the containers:

```bash
make deps      # only postgres + redis
make run-api   # go run ./cmd/api (in another terminal: make run-poller, make run-enricher)
make web-dev   # dashboard with hot reload at http://localhost:5173/admin (proxies to :8080)
make test lint # quality gates (same as CI)
```

`make help` lists every task.

## Public API

| Endpoint | Description |
|---|---|
| `GET /api/v1/now-playing` | Current playback (track, album, artists, progress). |
| `GET /api/v1/now-playing/stream` | Server-Sent Events, `event: now-playing` on every poll. |
| `GET /api/v1/now-playing/lyrics` | The full lyrics of the track playing now: `{ track_id, status, synced, instrumental, lines: [{ start_ms, text }], plain, credit, translation }`, synced lines when there are any, else the plain text. `status` is found, instrumental or pending (looked up soon); `translation` holds the English and Vietnamese Claude translation when enabled, with the translated song title and album name as `title` / `album: { en, vi }`. `null` when nothing plays or no lyrics were found (a paused track keeps its lyrics). |
| `GET /api/v1/history?limit=&cursor=` | Listening history, newest first, cursor pagination. |
| `GET /api/v1/tracks/{id}` | A played (or now playing) track with audio features, genres and play count (404 for a track never played), plus track and disc number, album type, size, UPC and copyrights, and the artists' Spotify genres. |
| `GET /api/v1/stats/summary?range=` | Totals: plays, unique tracks/artists, listening time. |
| `GET /api/v1/stats/top-tracks?range=&limit=` | Most played tracks. |
| `GET /api/v1/stats/top-artists?range=&limit=` | Most played artists. |
| `GET /api/v1/stats/genres?range=&limit=` | Genre breakdown (root + sub-genres, share of plays). |
| `GET /api/v1/stats/audio-profile?range=` | Play-weighted average audio features. |
| `GET /api/v1/playlists/recent?limit=` | Public playlists recently played from (name, cover, owner, plays). |
| `GET /api/v1/playlists/top?limit=` | Auto-generated "Top 50" playlists per `week` / `month` / `year`, newest first; `limit` (1–24, default 6) older entries per kind; only the newest carries its `tracks`. Each entry has `live` (the playlist of a period still running, still updating) and `refreshed_at` (UTC or `null`). |
| `POST /api/v1/events` | Cookieless page-view beacon from the public page (see `docs/admin-api.md`). |
| `GET /api/v1/listen/config` | Listen along: `{ enabled, clerk_publishable_key }` for the `/listen` page. |
| `POST /api/v1/listen/requests` | Listen along, public: "Request an invite" `{ name, email, message?, website? }`. Always 202 `{ ok: true }` for valid input (it reveals nothing about the email); 3 per hour per client, 422 on invalid input. Not in MCP. |
| `GET /api/v1/listen/me` | Listen along, signed-in invited listeners only (Clerk Bearer token): `{ email, name, image_url, admin }`. Not in MCP. |
| `GET /api/v1/listen/audio` | Listen along, signed-in invited listeners only: a short-lived presigned URL for the audio of the track playing now, `{ track_id, url, expires_at, content_type, reason }`. Not in MCP (see `docs/architecture.md`). |
| `POST /api/v1/listen/progress` | Listen along, signed-in invited listeners only: `{ track_id, heard_ms }`, audio time heard of the track playing now (204). A report that arrives just after the track changed still extends that song's play when it was reported within the last 30 s. Not in MCP. |
| `GET /api/v1/listen/history?limit=&cursor=` | Listen along, signed-in invited listeners only: the caller's own counted plays (30 s, or half a short song), newest first, `{ data, next_cursor }`. Not in MCP. |
| `POST /api/v1/listen/presence` | Listen along, signed-in invited listeners only: `{ state, track_id }` (listening, following or idle) for the admin's "Listening now" list (204, kept 90 s, no IP stored). Not in MCP. |

**Top 50 playlists.** When a week (ISO, Monday–Sunday), month or year ends, the
API creates a public Spotify playlist of the 50 most-played tracks of that
period (`TOP_PLAYLISTS_PERIODS`, boundaries in `TOP_PLAYLISTS_TIME_ZONE`) and
the public page embeds it. The current week also gets its playlist right away
(`TOP_PLAYLISTS_LIVE`, default `week`): it is refreshed hourly while you listen
(`TOP_PLAYLISTS_LIVE_REFRESH`) and finalized in place — same Spotify playlist —
15 minutes after the week ends. Only the newest three playlists per kind are
kept (`TOP_PLAYLISTS_KEEP`, the live one counts): older ones are deleted from
Spotify and dropped from the public page. Every hour (`TOP_PLAYLISTS_VERIFY`)
the kept playlists are checked against your Spotify library, and one you
deleted in the Spotify app is restored. Playlists are named
"{owner}'s Top 50 · Week of …", with the owner from `TOP_PLAYLISTS_OWNER` or
else your Spotify display name. It needs the `playlist-modify-public` scope:
re-authorize Spotify once in the admin (Settings → Spotify). The admin's
Data → Top 50 page lists every period with its status and track snapshot, runs
the generator on demand, backfills older periods, regenerates a playlist in
place (same Spotify playlist, new tracks) and hides or shows it on the public
page (see `docs/admin-api.md`). Local dev (`docker-compose.yml`, `.env.example`)
defaults `TOP_PLAYLISTS_PERIODS` to `none` so it never writes playlists to your
Spotify account; the production compose files default to `week,month,year`.

`range` is `7d`, `30d` (default), `90d`, `180d`, `365d` or `all`. Responses are
`{"data": …}`; errors are `{"error": {"code", "message"}}`. Stats are cached in
Redis (`CACHE_TTL`, default 5 min). The cache is dropped whenever a play is
recorded or a track is enriched, so it never serves outdated data. Responses
are sent with `Cache-Control: no-cache`, and tracks still being analysed with
`no-store`. CORS is limited to `CORS_ALLOWED_ORIGINS`
and `/api/v1` is rate limited per client IP.

## Use from Claude

The API serves a remote MCP server at `/mcp` with read-only tools for now
playing, history, track details, stats and playlists (see
[docs/architecture.md](docs/architecture.md#mcp-server)). Add it to Claude Code:

```sh
claude mcp add --transport http spotify-tracker https://listening.baoopn.com/mcp
```

or in `.mcp.json`:

```json
{
  "mcpServers": {
    "spotify-tracker": {
      "type": "http",
      "url": "https://listening.baoopn.com/mcp",
      "headers": { "Authorization": "Bearer ${SPOTIFY_TRACKER_MCP_TOKEN:-}" }
    }
  }
}
```

Without a token you get the public tools. For the read-only admin tools,
generate a token in the admin dashboard under **Settings → MCP** (it is shown
once; the page also has the filled-in commands), then pass it as
`--header "Authorization: Bearer <token>"` to `claude mcp add`, or set it for
`.mcp.json` (PowerShell: `setx SPOTIFY_TRACKER_MCP_TOKEN "<token>"`). Rotate or
revoke it on the same page. The server's `MCP_ADMIN_TOKEN`, if set, also works.
The admin tools include track lyrics, a lyrics search ("which song says
…"), the lyrics translation queue and a re-queue action. Lyrics come from
[LRCLIB](https://lrclib.net), translations from Claude. The public API serves
only the lyrics (and translation) of the song playing now
(`/api/v1/now-playing/lyrics`), and no public MCP tool returns lyrics.
claude.ai and Claude desktop custom connectors send no custom headers, so they
get the public tools only.

## Documentation

- [Architecture & design decisions](docs/architecture.md)
- [Deployment: Coolify, any VPS, CI/CD](docs/deployment.md)
- [Encrypted off-site backups & restore](docs/backup.md)
- [Admin dashboard & Clerk setup](docs/admin.md) · [Admin API contract](docs/admin-api.md)
- [Spotify developer setup & authorization](docs/spotify-token.md) (requires Spotify Premium)
- [Migrating from the Directus version](docs/migration-from-directus.md)

## License

MIT — see [LICENSE](LICENSE).
