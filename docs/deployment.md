# Deployment

Three compose files, one app image (`ghcr.io/baoopn/spotify-tracker`) plus
the backup sidecar (`ghcr.io/baoopn/spotify-tracker-backup`, see
[backup.md](backup.md)):

| File | Use |
|---|---|
| `docker-compose.yml` | Local development: builds the image, publishes ports on 127.0.0.1, debug logs. |
| `docker-compose.prod.yml` | Any Docker host (VPS): GHCR image, Caddy with automatic HTTPS, nightly local Postgres backups plus encrypted off-site backups, hardened containers, private network. |
| `docker-compose.coolify.yml` | Coolify: images built from Git on the server, Coolify-generated secrets, Traefik routing via `SERVICE_FQDN_API_8080`, encrypted off-site backups, the same container hardening as prod for api/poller/enricher. |

All stateful data lives in the bundled `postgres` and `redis` services and
their named volumes. No external database is needed.

## CI/CD (GitHub Actions)

**Workflow: pull requests only.** `main` is updated exclusively by merging a
PR with green CI. This is enforced by (1) a ruleset on `main` that requires a
PR and the CI checks, (2) a deploy gate in `release.yml` — a commit on `main`
that did not come from a merged PR is built and tested but **never
deployed** — and (3) a local pre-push hook (`git config core.hooksPath
.githooks`) that refuses pushes to `main`.

- **`ci.yml`** runs on PRs and on `main`. Go: generated-code drift check, `go mod tidy`
  check, golangci-lint, race tests, build. SPA: svelte-check, eslint, build.
  It also validates all compose files and does a Docker build on PRs. The
  backup image gets shellcheck, a build and a backup/restore roundtrip test. The
  **Public-safety check** (`scripts/check-public.sh`, also `make check-public`)
  fails if a tracked file contains a machine path, a key or token, deployment
  details (Coolify host, resource UUIDs, server IPs), a personal mailbox,
  `.env`/`.secrets/`, or ruflo runtime state. Real values live only in `.env`,
  `.secrets/` and the Coolify environment.
- **`release.yml`** runs on push to `main`, on `v*` tags, or manually.
  1. Runs CI.
  2. Builds the app image and the backup image (`<repo>-backup`) and pushes
     them to GHCR with the tags `sha-<short>`, `latest` (and the semver tag),
     plus provenance and SBOM.
  3. **Deploy** (environment `production`): triggers the Coolify resource
     through the Coolify API and waits until Coolify reports `finished` (the
     job fails if the deploy fails). Coolify builds the images itself from Git
     (branch `main`); the job does not set `IMAGE_TAG`. The deploy runs only if
     `main` still points at the tested commit; otherwise the newer run deploys.
     A `v*` tag deploys only when it tags `main`'s head (Coolify builds `main`,
     not the tag); otherwise the deploy is skipped with a notice. After the
     deploy, the job fails if Coolify reports that it built a different commit
     than the one CI tested.

Repository settings required:

| Kind | Name | Value |
|---|---|---|
| Secret | `COOLIFY_TOKEN` | Coolify API token (Keys & Tokens → API tokens, *deploy* + *write* scope) |
| Variable | `COOLIFY_URL` | `https://coolify.example.com` |
| Variable | `COOLIFY_RESOURCE_UUID` | UUID of the spotify-tracker application in Coolify |

Rollback: revert the change with a pull request (merging it deploys), or
use Coolify's rollback to redeploy an earlier deployment. Re-running an older
`Release & deploy` run does not roll back: Coolify builds the current `main`.

## Coolify

1. **No registry login.** Coolify builds the images from the Git repository
   on the target server, so it does not pull from GHCR.
2. **Create the resource.** Project → Environment → *New resource* → *Docker
   Compose* (from the Git repository `baoopn/spotify-tracker`, branch `main`),
   Docker Compose location `/docker-compose.coolify.yml`, destination server of your choice.
3. **Domain.** On the `api` service, set the domain, e.g.
   `https://listening.example.com` (the whole project: `/`, `/api`, `/admin`).
   Traefik terminates TLS. The DNS record must point at that server.
4. **Environment variables.** Coolify generates `SERVICE_*`. Set:
   `SECRETS_KEY`, `SPOTIFY_CLIENT_ID`, `SPOTIFY_CLIENT_SECRET`
   (+ `SPOTIFY_REFRESH_TOKEN`, `SPOTIFY_AUTHORIZED_AT` to migrate the current
   token), `SOUNDCHARTS_APP_ID`, `SOUNDCHARTS_TOKEN`, `CORS_ALLOWED_ORIGINS`,
   `CLERK_PUBLISHABLE_KEY`, `CLERK_SECRET_KEY`, `ADMIN_ALLOWED_EMAILS`,
   `EMAIL_SMTP_*`, `EMAIL_FROM`, `ALERT_EMAIL_TO`, and optionally
   `POLL_INTERVAL_MS`, `ENRICH_INTERVAL_MS`, `SOUNDCHARTS_RPS`.
   If Cloudflare proxies the domain, set `CLIENT_IP_HEADER=CF-Connecting-IP`.
   **Moving a domain from another server:** Traefik requests the Let's
   Encrypt certificate when the app first deploys. If DNS still points
   elsewhere at that moment the request fails and, with Cloudflare SSL mode
   *Full (strict)*, visitors get **526** after the DNS switch. Fix: switch
   DNS, then **Restart** the application in Coolify — Traefik retries and the
   certificate is issued within a minute (verified 2026-11-05).
5. **Spotify.** Add `https://<domain>/admin/api/spotify/callback` to the
   app's Redirect URIs in the Spotify Developer Dashboard.
6. **Backups.** The `backup` service sends an encrypted dump to a private
   R2 bucket (or any S3 store) every day. Create the bucket, a bucket-scoped
   token and an age key pair, then set `BACKUP_AGE_RECIPIENT` and
   `BACKUP_S3_*`; see [backup.md](backup.md). Until then the service idles
   with a warning. Restore works on any host, not only Coolify.
7. Copy the resource UUID into the GitHub variable `COOLIFY_RESOURCE_UUID`.
   From now on every push to `main` deploys automatically.

## Any VPS (Docker Engine + Compose v2)

```bash
git clone https://github.com/baoopn/spotify-tracker && cd spotify-tracker
cp .env.example .env
# Set APP_ENV=production, API_DOMAIN, PUBLIC_URL=https://$API_DOMAIN,
# POSTGRES_PASSWORD, REDIS_PASSWORD, INTERNAL_API_TOKEN, SECRETS_KEY
# (openssl rand -hex 32 for each), plus Spotify/Soundcharts/Clerk/SMTP values.
export IMAGE_TAG=sha-xxxxxxx   # required: the sha- tag of a release build
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

`IMAGE_TAG` has no default: the stack refuses to start without it, so a host
never runs a moving `latest` by accident. Pick a `sha-<short>` tag from GHCR
(every release run pushes one).

`.env` is only read by Compose to fill in the `${...}` values; no container
loads it whole. Each service lists the variables it needs (the same split as
`docker-compose.coolify.yml`): `api` gets the database, Redis, secrets key,
Spotify, Clerk, SMTP and Top 50 settings; `poller` only Redis, the internal
token and `POLL_INTERVAL_MS`; `enricher` Redis, the internal token and the
Soundcharts and `ENRICH_*` settings; the importer adds `DIRECTUS_*`. A setting
added to `.env` reaches a container only after it is added to that service's
`environment` in `docker-compose.prod.yml`.

Point the domain's DNS A/AAAA records at the server and open ports 80/443.
Caddy obtains certificates automatically. Backups are written daily to
`./backups` (7 daily, 4 weekly, 6 monthly). Restore with:

```bash
gunzip -c backups/daily/spotify_tracker-YYYYMMDD.sql.gz | \
  docker compose -f docker-compose.prod.yml exec -T postgres psql -U tracker spotify_tracker
```

The `offsite-backup` service also sends an encrypted copy to R2 (or any S3
store) once you set `BACKUP_AGE_RECIPIENT` and `BACKUP_S3_*` in `.env`. See
[backup.md](backup.md) for setup and restore. If the backup image on GHCR is
private, run `docker login ghcr.io` first, or build it on the host.

Upgrade: `IMAGE_TAG=sha-xxxxxxx docker compose -f docker-compose.prod.yml up -d`
(or `IMAGE_TAG=sha-xxxxxxx make prod-up`).

The Top 50 playlist generator (`TOP_PLAYLISTS_PERIODS`) defaults to `none` in
local dev and to `week,month,year` in the production compose files.

## Operations

- Logs: JSON on stdout (`docker compose logs -f api`).
- Health: every container has a healthcheck. `GET /healthz` on the API is public;
  `/readyz` and `/metrics` stay on the internal ops port 9090.
- Scale: `poller` must run as a single replica. `enricher` and `api` can be scaled
  (the advisory lock makes migrations safe; the Redis consumer group shares work).
