# Deployment

Three compose files, one image (`ghcr.io/baoopn/spotify-tracker`):

| File | Use |
|---|---|
| `docker-compose.yml` | Local development: builds the image, publishes ports on 127.0.0.1, debug logs. |
| `docker-compose.prod.yml` | Any Docker host (VPS): GHCR image, Caddy with automatic HTTPS, nightly Postgres backups, hardened containers, private network. |
| `docker-compose.coolify.yml` | Coolify: GHCR image, Coolify-generated secrets, Traefik routing via `SERVICE_FQDN_API_8080`. |

All stateful data lives in the bundled `postgres` and `redis` services and
their named volumes. No external database is needed.

## CI/CD (GitHub Actions)

- **`ci.yml`** runs on PRs and on `main`. Go: generated-code drift check, `go mod tidy`
  check, golangci-lint, race tests, build. SPA: svelte-check, eslint, build.
  It also validates all compose files and does a Docker build on PRs.
- **`release.yml`** runs on push to `main`, on `v*` tags, or manually.
  1. Runs CI.
  2. Builds the image and pushes it to GHCR with the tags `sha-<short>`,
     `latest` (and the semver tag), plus provenance and SBOM.
  3. **Deploy** (environment `production`): sets `IMAGE_TAG=sha-<short>` on the
     Coolify resource through the Coolify API, triggers a deployment, and waits
     until Coolify reports `finished` (the job fails if the deploy fails).

Repository settings required:

| Kind | Name | Value |
|---|---|---|
| Secret | `COOLIFY_TOKEN` | Coolify API token (Keys & Tokens → API tokens, *deploy* + *write* scope) |
| Variable | `COOLIFY_URL` | `https://coolify.baoopn.com` |
| Variable | `COOLIFY_RESOURCE_UUID` | UUID of the spotify-tracker application in Coolify |

Rollback: re-run an older successful `Release & deploy` run, or set
`IMAGE_TAG` to an older `sha-…` in Coolify and redeploy.

## Coolify (server `sr2`)

1. **GHCR access.** If the package is private, add a registry login for
   `ghcr.io` on the sr2 server (Coolify → Servers → sr2 → Docker registries)
   with a read-only PAT (`read:packages`).
2. **Create the resource.** Project → Environment → *New resource* → *Docker
   Compose* (from the Git repository `baoopn/spotify-tracker`, branch `main`),
   Docker Compose location `/docker-compose.coolify.yml`, destination server **sr2**.
3. **Domain.** On the `api` service, set the domain, e.g.
   `https://listening.baoopn.com` (the whole project: `/`, `/api`, `/admin`).
   Traefik terminates TLS. The DNS record must point at the sr2 server.
4. **Environment variables.** Coolify generates `SERVICE_*`. Set:
   `SECRETS_KEY`, `SPOTIFY_CLIENT_ID`, `SPOTIFY_CLIENT_SECRET`
   (+ `SPOTIFY_REFRESH_TOKEN`, `SPOTIFY_AUTHORIZED_AT` to migrate the current
   token), `SOUNDCHARTS_APP_ID`, `SOUNDCHARTS_TOKEN`, `CORS_ALLOWED_ORIGINS`,
   `CLERK_PUBLISHABLE_KEY`, `CLERK_SECRET_KEY`, `ADMIN_ALLOWED_EMAILS`,
   `EMAIL_SMTP_*`, `EMAIL_FROM`, `ALERT_EMAIL_TO`, and optionally
   `POLL_INTERVAL_MS`, `ENRICH_INTERVAL_MS`, `SOUNDCHARTS_RPS`.
   If Cloudflare proxies the domain, set `CLIENT_IP_HEADER=CF-Connecting-IP`.
5. **Spotify.** Add `https://<domain>/admin/api/spotify/callback` to the
   app's Redirect URIs in the Spotify Developer Dashboard.
6. **Backups.** Enable scheduled backups for the `postgres` service in Coolify
   (Service → postgres → Backups), ideally to S3.
7. Copy the resource UUID into the GitHub variable `COOLIFY_RESOURCE_UUID`.
   From now on every push to `main` deploys automatically.

## Any VPS (Docker Engine + Compose v2)

```bash
git clone https://github.com/baoopn/spotify-tracker && cd spotify-tracker
cp .env.example .env
# Set APP_ENV=production, API_DOMAIN, PUBLIC_URL=https://$API_DOMAIN,
# POSTGRES_PASSWORD, REDIS_PASSWORD, INTERNAL_API_TOKEN, SECRETS_KEY
# (openssl rand -hex 32 for each), plus Spotify/Soundcharts/Clerk/SMTP values.
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

Point the domain's DNS A/AAAA records at the server and open ports 80/443.
Caddy obtains certificates automatically. Backups are written daily to
`./backups` (7 daily, 4 weekly, 6 monthly). Restore with:

```bash
gunzip -c backups/daily/spotify_tracker-YYYYMMDD.sql.gz | \
  docker compose -f docker-compose.prod.yml exec -T postgres psql -U tracker spotify_tracker
```

Upgrade: `IMAGE_TAG=sha-xxxxxxx docker compose -f docker-compose.prod.yml up -d`
(or `make prod-up` for `latest`).

## Operations

- Logs: JSON on stdout (`docker compose logs -f api`).
- Health: every container has a healthcheck. `GET /healthz` on the API is public;
  `/readyz` and `/metrics` stay on the internal ops port 9090.
- Scale: `poller` must run as a single replica. `enricher` and `api` can be scaled
  (the advisory lock makes migrations safe; the Redis consumer group shares work).
