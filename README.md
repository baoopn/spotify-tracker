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
| `cmd/poller` | Polls Spotify *currently playing* every 3 s and forwards snapshots over gRPC. Holds no Spotify secrets: access tokens come from the API. |
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
| `GET /api/v1/history?limit=&cursor=` | Listening history, newest first, cursor pagination. |
| `GET /api/v1/tracks/{id}` | Track with audio features, genres and play count. |
| `GET /api/v1/stats/summary?range=` | Totals: plays, unique tracks/artists, listening time. |
| `GET /api/v1/stats/top-tracks?range=&limit=` | Most played tracks. |
| `GET /api/v1/stats/top-artists?range=&limit=` | Most played artists. |
| `GET /api/v1/stats/genres?range=&limit=` | Genre breakdown (root + sub-genres, share of plays). |
| `GET /api/v1/stats/audio-profile?range=` | Play-weighted average audio features. |
| `GET /api/v1/playlists/recent?limit=` | Public playlists recently played from (name, cover, owner, plays). |
| `POST /api/v1/events` | Cookieless page-view beacon from the public page (see `docs/admin-api.md`). |

`range` is `7d`, `30d` (default), `90d`, `180d`, `365d` or `all`. Responses are
`{"data": …}`; errors are `{"error": {"code", "message"}}`. Stats are cached in
Redis (`CACHE_TTL`, default 5 min). The cache is dropped whenever a play is
recorded or a track is enriched, so it never serves outdated data. Responses
are sent with `Cache-Control: no-cache`, and tracks still being analysed with
`no-store`. CORS is limited to `CORS_ALLOWED_ORIGINS`
and `/api/v1` is rate limited per client IP.

## Documentation

- [Architecture & design decisions](docs/architecture.md)
- [Deployment: Coolify, any VPS, CI/CD](docs/deployment.md)
- [Admin dashboard & Clerk setup](docs/admin.md) · [Admin API contract](docs/admin-api.md)
- [Spotify developer setup & authorization](docs/spotify-token.md) (requires Spotify Premium)
- [Migrating from the Directus version](docs/migration-from-directus.md)
