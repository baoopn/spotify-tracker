# Directus Collections Setup

## Overview

- This document describes how to create and configure the Directus collections used by the Spotify Tracker project:
  - `artists` (entity)
  - `albums` (entity)
  - `tracks` (entity)
  - `track_artists` (M2M junction — auto-created by Directus)
  - `current_listening` (singleton)
  - `listening_history` (append-only log)
  - `track_features` (audio features metadata)

## Collections & Fields

- **artists**
  - `id`: string (primary — Spotify artist ID)
  - `name`: string
  - `uri`: string (optional)
  - `external_url`: string (optional)

- **albums**
  - `id`: string (primary — Spotify album ID)
  - `name`: string
  - `release_date`: string (optional)
  - `cover_image`: string (optional — primary image URL)
  - `uri`: string (optional)
  - `external_url`: string (optional)

- **tracks**
  - `id`: string (primary — Spotify track ID)
  - `name`: string
  - `uri`: string (optional)
  - `isrc`: string (optional)
  - `duration_ms`: integer
  - `explicit`: boolean (optional)
  - `external_url`: string (optional)
  - `album_id`: M2O → `albums` (optional)
  - `artists`: M2M → `artists` via `track_artists` junction (Directus creates this automatically)

- **current_listening** (singleton)
  - `id`: uuid (primary)
  - `track_id`: M2O → `tracks`
  - `is_playing`: boolean
  - `progress_ms`: integer (nullable)
  - `fetched_at`: datetime
  - Notes: mark collection as Singleton in Collection Settings.

- **listening_history** (log)
  - `id`: uuid (primary)
  - `track_id`: M2O → `tracks` (index)
  - `played_at`: datetime
  - `updated_at`: datetime (optional)
  - `duration_ms`: integer (optional)
  - `replay_count`: integer — default `1` (optional)

- **track_features** (metadata)
  - `id`: uuid (primary)
  - `track_id`: M2O → `tracks` (unique; add unique index)
  - `isrc`: string (optional — ISRC code)
  - `acousticness`: number (0–1)
  - `danceability`: number (0–1)
  - `energy`: number (0–1)
  - `instrumentalness`: number (0–1)
  - `key`: integer (0–11 or null)
  - `liveness`: number (0–1)
  - `loudness`: number (dB)
  - `mode`: integer (0 or 1 or null)
  - `speechiness`: number (0–1)
  - `tempo`: number (BPM)
  - `valence`: number (0–1)
  - `genres`: json/array — provider genres (e.g. [{"root":"alternative","sub":["alternative"]}])
  - `enriched`: boolean — default `false`
  - `enriched_at`: datetime (optional)

## Directus Admin GUI: Step-by-step

1. Sign in to Directus Admin.
2. Settings → Collections & Fields → Create Collection.
3. Create `artists` with the fields above.
4. Create `albums` with the fields above.
5. Create `tracks`:
  - Add scalar fields (`name`, `uri`, `isrc`, `duration_ms`, `explicit`, `external_url`).
   - Add a **Many-to-One** relation field `album_id` pointing to `albums`.
   - Add a **Many-to-Many** relation field `artists` pointing to `artists` (Directus creates the `track_artists` junction automatically).
6. Create `current_listening`. Add a **Many-to-One** relation field `track_id` → `tracks`, then add remaining fields. Open Collection Settings and toggle **Singleton**.
7. Create `listening_history`. Add a **Many-to-One** relation field `track_id` → `tracks`, then add remaining fields.
8. Create `track_features`. Add a **Many-to-One** relation field `track_id` → `tracks`, then add scalar fields (`isrc`, `acousticness`, `danceability`, `energy`, `instrumentalness`, `key`, `liveness`, `loudness`, `mode`, `speechiness`, `tempo`, `valence`, `enriched`, `enriched_at`).
  - Also add `genres` as a JSON/Array field to store provider genre data (e.g. Soundcharts `[{"root":"alternative","sub":["alternative"]}]`).
  - In Directus, use the `JSON` field type (or `text` if JSON not available) and allow `null` values.
9. For `track_features.track_id`, enable **Unique** in the field's Validation options to enforce One-to-One cardinality (Directus implements O2O as M2O with unique constraint).

## Permissions (roles)

- Create a `worker-poll` role:
  - `artists`: allow `create`, `read`, `update`.
  - `albums`: allow `create`, `read`, `update`.
  - `tracks`: allow `create`, `read`, `update`.
  - `current_listening`: allow `read`, `update`.
  - `listening_history`: allow `create`.
  - `track_features`: allow `read` only (to avoid poll worker writing enrichment fields).
- Create a `worker-enrich` role:
  - `track_features`: allow `create`, `read`, `update`.
  - `listening_history`: allow `read` (for lookup/backfill if needed).
  - `tracks`: allow `read`.
  - `artists`, `albums`, `current_listening`: no access unless your enrich logic explicitly needs them.
- Configure `public` or `frontend` role:
  - Allow `read` on `artists`, `albums`, `tracks`, `current_listening`, and `track_features`.
  - Optionally allow limited `read` on recent `listening_history` (use filters/permissions to limit scope).

## Worker Users & Tokens

1. Go to Settings → Access Control → Roles, then create roles `worker-poll` and `worker-enrich` with the permissions above.
2. Go to User Directory → Create User:
  - Email: `worker-poll@local`
  - Role: `worker-poll`
  - Status: Active
3. Create another user:
  - Email: `worker-enrich@local`
  - Role: `worker-enrich`
  - Status: Active
4. Go to Settings → API Tokens → Create Token:
  - Token name: `worker-poll-token`
  - Role: `worker-poll`
5. Create a second token:
  - Token name: `worker-enrich-token`
  - Role: `worker-enrich`
6. Set env vars in your workers:
  - Poll worker: `DIRECTUS_TOKEN=<worker-poll-token>`
  - Enrich worker: `DIRECTUS_TOKEN=<worker-enrich-token>`

## Flows / Webhooks (triggering enrichment)

- Option A — Flow (recommended):
  1. Settings → Flows → Create Flow.
  2. Add Trigger: Event `Items → Create` on `listening_history` (or `Items → Update` for `track_features` where `enriched=false`).
  3. Add Operation: HTTP Request to your Enricher worker endpoint, e.g. `https://worker-enrich.example.com/api/enrich`.
  4. Body: send `track_id` or the item's payload (`{{payload}}`).
  5. Save & enable the Flow.

- Option B — Webhook: Settings → Webhooks → Create Webhook for `items.create` on `listening_history`.

## API Examples

- Use a service API token (create in Directus Admin → Settings → API → Tokens) and include `Authorization: Bearer <TOKEN>`.

Upsert a track (and its relations) before inserting history:

```bash
# 1. Upsert artist
curl -X POST 'https://your-directus.example.com/items/artists' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{"id":"<artist_id>","name":"Artist Name","external_url":"https://open.spotify.com/artist/<id>"}'

# 2. Upsert album
curl -X POST 'https://your-directus.example.com/items/albums' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{"id":"<album_id>","name":"Album Name","cover_image":"https://i.scdn.co/image/<hash>"}'

# 3. Upsert track (with M2O album and M2M artists)
curl -X POST 'https://your-directus.example.com/items/tracks' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{"id":"<track_id>","name":"Track Name","duration_ms":215000,"album_id":"<album_id>","artists":[{"artists_id":"<artist_id>"}]}'
```

Insert listening history (using track relation):

```bash
curl -X POST 'https://your-directus.example.com/items/listening_history' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{"track_id":"<track_id>","played_at":"2026-03-16T12:34:56Z","duration_ms":215000}'
```

Read current listening with all relations hydrated:

```bash
curl 'https://your-directus.example.com/items/current_listening?fields=*,track_id.*,track_id.album_id.*,track_id.artists.artists_id.*' \
  -H 'Authorization: Bearer <TOKEN>'
```

Read track features for a specific track:

```bash
curl 'https://your-directus.example.com/items/track_features?filter[track_id]=<track_id>' \
  -H 'Authorization: Bearer <TOKEN>'
```

Upsert track features (with audio features from Soundcharts):

```bash
curl -X PATCH 'https://your-directus.example.com/items/track_features' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '[{"track_id":"<track_id>","isrc":"FRX762579671","acousticness":0.574,"danceability":0.546,"energy":0.312,"instrumentalness":1.85E-6,"key":8,"liveness":0.369,"loudness":-11.633,"mode":1,"speechiness":0.0502,"tempo":177.997,"valence":0.575,"genres":[{"root":"alternative","sub":["alternative"]}],"enriched":true}]'
```

## Notes & Best Practices

- Audio features (acousticness, danceability, energy, etc.) are stored as individual columns in `track_features` for easy filtering and querying.
- Each audio feature field is nullable to represent missing provider records.
- Use a limited `service` role + API token for workers; restrict to necessary operations only.
- Add indexes on `track_id` in `listening_history` and `track_features` for fast lookups.
- Implement idempotency in workers: upsert `artists`, `albums`, and `tracks` before inserting `listening_history`.
- For lower latency, use Flows/webhooks to notify the enricher rather than polling.
- When querying with Directus SDK, use `fields: ['*', 'track_id.*', 'track_id.album_id.*', 'track_id.artists.artists_id.*']` to hydrate all relations.

## Seeding & Verification

1. Upsert a test artist, album, and track using the curl examples above.
2. Insert a `listening_history` test record referencing the track.
3. Verify a Flow/webhook triggers your enricher endpoint.
4. Confirm `current_listening` updates and `track_features` entries are populated with individual audio feature columns when Soundcharts data is available.
