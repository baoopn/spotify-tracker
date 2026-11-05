# Migrating from the Directus version

The previous version stored data in Directus collections (`artists`,
`albums`, `tracks` + `tracks_artists`, `track_features`, `listening_history`,
`current_listening`). `cmd/importer` copies everything into PostgreSQL through
the Directus REST API. It is **idempotent**: catalog rows are upserted, and
history rows are deduplicated on `(track_id, played_at)`. That means you can
run it while the old workers are still recording, and run it again right
before switching over.

## Configuration

```env
DIRECTUS_URL=https://api.example.com
# One token with read access to all six collections, or several tokens
# (comma-separated). Each request tries them in order, so the legacy
# worker-poll and worker-enrich tokens can be combined.
DIRECTUS_TOKEN=<poll token>,<enrich token>
```

## Modes

| Mode | What is imported |
|---|---|
| `-mode=enriched` (default) | Only **enriched** `track_features` rows plus exactly the tracks, albums, artists and track↔artist links they reference. **No listening history, no current playback.** |
| `-mode=all` | Every collection, including listening history (dev/testing only). |

Duplicate handling in `enriched` mode: Directus rows are de-duplicated by
Spotify ID (for duplicate feature rows the most complete one wins); catalog
writes are upserts that keep existing non-empty fields; features are only
written when the new database does **not** already have that track enriched
(`pending`, `failed`, `unavailable` or missing rows are filled; `enriched`
rows are kept). Re-running is safe.

## Run

```bash
# local dev database
make import-directus
# production on a VPS
docker compose -f docker-compose.prod.yml --profile tools run --rm importer
# Coolify: run once in the api container's terminal
/usr/local/bin/importer
# read-only rehearsal
importer -dry-run
```

The summary log line reports counts per collection. Tracks the old system
never enriched are imported as `pending`, and the enricher picks them up
(about 2,600 tracks at 10 req/s takes roughly 5 minutes).

## Cut-over checklist

1. Deploy the new stack with the Spotify credentials (seed
   `SPOTIFY_REFRESH_TOKEN` / `SPOTIFY_AUTHORIZED_AT`).
2. Run the importer.
3. Stop the old Directus-based poller and enricher containers.
4. Run the importer once more to pick up anything recorded in between.
5. Point the frontend at the new `/api/v1` API.
6. Keep Directus running only if other projects use it.
