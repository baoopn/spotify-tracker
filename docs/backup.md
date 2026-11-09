# Off-site backups

A small sidecar container (`deploy/backup/`, image
`ghcr.io/baoopn/spotify-tracker-backup`) dumps the Postgres database once a
day, encrypts the dump with [age](https://age-encryption.org), and uploads it
to a private Cloudflare R2 bucket. Any S3-compatible store or any
[rclone](https://rclone.org) remote works too.

The server only holds the **public** key. The private key stays with you and
is needed only to restore. A stolen server or a leaked bucket token does not
expose the data.

Restore works the same everywhere: Coolify, the VPS compose stack, a laptop,
or a machine without Docker.

## What gets backed up

- The whole application database (`spotify_tracker`): every table, including
  listening history, tracks, playlists, settings and `goose_db_version`.
- Format: `pg_dump --format=custom --compress=6 --no-owner --no-privileges`,
  piped into `age`. The plaintext dump never touches the storage, and it is
  never written to the container's disk either: backup, verify and restore
  stream it through pipes. Only the ciphertext is staged in `/tmp`.
- A backup is refused (nothing is uploaded) when the dump contains no
  tables, for example when it points at the wrong database.
- Not backed up: Redis (cache and work queue only; it refills by itself).

The database stores the Spotify refresh token and the SMTP password encrypted
with `SECRETS_KEY`. Keep a copy of `SECRETS_KEY` with your age key. Without
it a restored database still works, but you must re-authorize Spotify and
re-enter the SMTP password.

Each backup is two objects:

```
<prefix>/<YYYY>/<MM>/spotify-tracker-20261109T030000Z.dump.age        encrypted dump
<prefix>/<YYYY>/<MM>/spotify-tracker-20261109T030000Z.dump.age.json   manifest
```

The manifest holds the name, time, size, the sha256 of the encrypted file,
the pg_dump and server versions, the database name and the schema (goose)
version. Restore checks the sha256 before it decrypts anything.

The sha256 proves the file is intact (no damage, no truncation). It does
not prove who made it: anyone with write access to the bucket could upload a
new backup with a matching manifest, because age encryption uses only your
public key. Keep the bucket token private and scoped to this one bucket.

## Setup

### 0. Make the image pullable (once)

The release workflow pushes `ghcr.io/baoopn/spotify-tracker-backup` with the
same tags as the app. GitHub creates a new package as **private**. For the
`docker run` recipes below (and VPS hosts) to pull it without a login, open
the package on GitHub → *Package settings* → *Change visibility* → *Public*,
once. Otherwise run `docker login ghcr.io` with a token that has
`read:packages`. Coolify builds the image from the repository and needs
neither.

### 1. Create a private bucket and a token (Cloudflare R2)

1. Cloudflare dashboard → R2 → **Create bucket**. Pick a name, keep it
   private (no public access, no custom domain).
2. R2 → **Manage API tokens** → **Create API token**:
   - Permission: **Object Read & Write**.
   - Scope: **Apply to specific buckets only**, select the new bucket.
3. Copy the **Access Key ID**, the **Secret Access Key** and the
   **S3 endpoint** (`https://<account id>.r2.cloudflarestorage.com`).

A bucket-scoped token cannot list or create buckets. The tool never needs to.

Other providers: set `BACKUP_S3_PROVIDER` (rclone's name: `AWS`, `Minio`,
`Wasabi`, `Backblaze`, ...) and `BACKUP_S3_REGION`. For anything else, set
`BACKUP_REMOTE` to an rclone path and pass the remote's settings as
`RCLONE_CONFIG_<NAME>_*` variables.

### 2. Create the age key pair

On your own machine:

```bash
docker run --rm ghcr.io/baoopn/spotify-tracker-backup keygen > backup-age-key.txt
```

The private key goes to `backup-age-key.txt`. The public recipient
(`age1...`) is printed on the terminal. Store the key file somewhere safe and
offline, for example in your password manager. **If you lose it, the backups
cannot be decrypted.** Do not put it on the server.

You can also use `age-keygen -o backup-age-key.txt` if you have age
installed. To encrypt for more than one key (say, a second person), list
several recipients separated by spaces or commas.

### 3. Configure the service

| Variable | Value |
|---|---|
| `BACKUP_AGE_RECIPIENT` | the `age1...` public key |
| `BACKUP_S3_ENDPOINT` | `https://<account id>.r2.cloudflarestorage.com` |
| `BACKUP_S3_BUCKET` | bucket name |
| `BACKUP_S3_ACCESS_KEY_ID` | token access key ID |
| `BACKUP_S3_SECRET_ACCESS_KEY` | token secret |

- **Coolify:** add them in the resource's *Environment Variables* tab and
  redeploy. The `backup` service is part of `docker-compose.coolify.yml`.
- **VPS:** add them to `.env` and run
  `docker compose -f docker-compose.prod.yml up -d offsite-backup`. The
  local `backup` service (daily dumps in `./backups`) keeps running too.
- **Local dev:** `docker compose --profile backup ...` writes to a Docker
  volume, no bucket needed. See the comments in `docker-compose.yml`.

When no `BACKUP_*` setting is set at all, the service logs a warning, idles
and stays healthy, so installs without off-site backups keep working. When
only some are set (a destination without a recipient, or one to three of the
four `BACKUP_S3_*` values), it logs an ERROR, pings `<url>/fail` if
`BACKUP_PING_URL` is set, and reports **unhealthy**. It never uploads an
unencrypted dump.

Check it works: run one backup now and list the bucket.

```bash
# Coolify: open the backup container's terminal; VPS: prefix with
# docker compose -f docker-compose.prod.yml exec offsite-backup
backupctl backup
backupctl list
```

## Schedule, retention and monitoring

- One backup a day at `BACKUP_TIME` (UTC, default `03:00`), or at the time
  set in the admin dashboard.
  `BACKUP_ON_START=true` also runs one when the container starts.
- A failed run is retried once after `BACKUP_RETRY_MINUTES` (default 30).
  The container never exits because of a failed backup.
- After each successful backup, backups older than
  `BACKUP_KEEP_DAYS` (default 30) are deleted. The newest
  `BACKUP_KEEP_MIN` (default 7) are always kept, however old. The newest
  backup is never deleted, so a value below 1 counts as 1.
  `BACKUP_KEEP_DAYS=0` keeps everything. The admin dashboard can override
  both values. You can also set a lifecycle rule on the bucket.
- The Docker healthcheck turns **unhealthy** when there was no successful
  backup in `BACKUP_MAX_AGE_HOURS` (default 26). It stays healthy while the
  service idles or waits for its first run.
- `BACKUP_PING_URL` (optional): a GET after each success and
  `<url>/fail` after each failure, as [healthchecks.io](https://healthchecks.io)
  expects. **This is the real monitor**: the Docker healthcheck only knows
  what happened since the container started (a redeploy resets it), and
  nobody gets an email from it. A free healthchecks.io check with a 1-day
  period and a few hours of grace emails you when backups stop.

## Admin dashboard

The admin dashboard (*Settings → Backups*) shows the backup status and can
start a backup. The api has no storage credentials, so the backup service
and the api talk only through the database (migration `00011`):

| Table | Written by | Holds |
|---|---|---|
| `backup_agent` | backup service, about once a minute | heartbeat: effective time, retention, pause, next run, redacted destination (`s3:<bucket>/<prefix>` or `remote:<name>/<prefix>`), age recipients, config problems |
| `backup_runs` | backup service | one row per backup: trigger, status, times, name, size, sha256, table count, short error |
| `backup_inventory` | backup service | the backups in storage, refreshed after each backup and prune |
| `backup_requests` | api (admin) | queued "back up now" and "refresh list" requests |

No secret goes into these tables: no storage keys, no endpoint, no private
key. Recipients are public keys.

- The scheduler wakes every minute. It writes the heartbeat, reads the
  admin settings, and runs the oldest queued request.
- Admin settings live in the `settings` table under key `backup`, as plain
  JSON: `{"time":"03:00","keep_days":30,"keep_min":7,"paused":false}`.
  Each field is optional. A missing field falls back to the env value
  (`BACKUP_TIME`, `BACKUP_KEEP_DAYS`, `BACKUP_KEEP_MIN`), then to the
  default. The service checks the values (time `HH:MM`, keep days 0 to
  3650, keep at least 1 to 1000). If they are not valid, it logs a WARN and
  keeps the values it had. Changes apply within a minute.
- **Paused** stops the daily backups. "Back up now" still works, and the
  healthcheck stays healthy.
- When the container stops during a backup (`docker stop`, a redeploy), it
  drops the backup and marks the run and its request `interrupted`. If it
  had no time for that (killed, crashed), the next start marks runs and
  requests older than 2 hours. Newer ones are left alone, because during a
  rolling deploy the old container may still be at work.
- After a restore, the status rows in the target are the ones from the
  moment of the backup. `restore` marks the runs and requests that were open
  then as interrupted, and removes the old heartbeat. The running service
  writes a new one within a minute.
- If the status tables appear after the service started (the api ran the
  migrations later), the service records a backup it made before then and
  fills the inventory.
- The service deletes runs older than 180 days and done requests older
  than 30 days.
- The status is best-effort. If a status write fails (the database is busy,
  or an older database has no status tables), the service logs a WARN and
  the backup goes on as before.
- `backupctl backup` from a shell is recorded too, as a manual run.
- Without settings, or with partial settings, the service still writes the
  heartbeat, so the dashboard can show what is missing. Queued requests
  then fail with that reason.

Test a restore from time to time. `backupctl verify latest` downloads,
checks and decrypts the newest backup without touching any database:

```bash
docker run --rm --env-file backup.env \
  -v "$PWD/backup-age-key.txt:/run/secrets/backup-age-key:ro" --user "$(id -u)" \
  ghcr.io/baoopn/spotify-tracker-backup verify latest
```

## Commands

```
backupctl run                 scheduler (the default command)
backupctl backup              one backup now
backupctl list                backups, newest first
backupctl verify  [latest|NAME] [-i KEY]
backupctl restore [latest|NAME] [--force] [--to-file PATH] [-i KEY]
backupctl prune               apply the retention rules now
backupctl keygen              new key pair
```

`-i KEY` is the age private key file. The default is
`/run/secrets/backup-age-key` (or `BACKUP_AGE_IDENTITY_FILE`). `-i -` reads it
from stdin into memory and hands it to age through a pipe, so the key is
never written to disk, on the server or in the container.

`restore` refuses to write into a database that already has tables in schema
`public`. `--force` drops and replaces the objects that are in the backup
(`pg_restore --clean --if-exists`). The restore runs in a single
transaction: if it fails, nothing changes. `--to-file PATH` only writes the
decrypted `.dump` file, for use with `pg_restore` by hand. It is the only
command that writes plaintext to disk (mode 600); delete the file when done.

The database to restore into comes from `DATABASE_URL` or the usual `PGHOST`,
`PGPORT`, `PGUSER`, `PGPASSWORD` and `PGDATABASE`. Point them at another
database to restore there.

## Restore

The order matters in a disaster:

1. Start **only** Postgres (a new, empty database).
2. Restore the backup into it.
3. Start the app. At startup the api runs the goose migrations, so a backup
   made by an older version is migrated forward.

If the app already started on the new database, it created empty tables, and
`restore` refuses. Stop the app and either restore with `--force` (fine when
the app version matches the backup) or, for a clean slate, empty the schema
first:

```sql
DROP SCHEMA public CASCADE; CREATE SCHEMA public;
```

In the examples, `backup-age-key.txt` is your private key and the backup is
`latest` (or a name from `backupctl list`).

### (a) Coolify

From an SSH session on the server (the key is read from stdin and never
saved):

```bash
docker ps --format '{{.Names}}' | grep -E '^(api|poller|enricher|backup)-'
docker stop <api-container> <poller-container> <enricher-container>
docker exec -i <backup-container> backupctl restore latest -i - < backup-age-key.txt
#   add --force if the database already has tables (see above)
```

Then **Restart** the resource in Coolify. On a new server: deploy the
resource with the same `BACKUP_*` variables, stop the app containers as
above, restore with `--force` (or empty the schema first), and restart.

### (b) VPS (docker-compose.prod.yml)

```bash
docker compose -f docker-compose.prod.yml up -d postgres
docker compose -f docker-compose.prod.yml stop api poller enricher   # if running
docker compose -f docker-compose.prod.yml run --rm -T offsite-backup \
  restore latest -i - < backup-age-key.txt
docker compose -f docker-compose.prod.yml up -d
```

### (c) Any machine with Docker

Put the settings in a file so secrets stay out of your shell history:

```bash
cat > backup.env <<'EOF'
BACKUP_S3_ENDPOINT=https://<account id>.r2.cloudflarestorage.com
BACKUP_S3_BUCKET=<bucket>
BACKUP_S3_ACCESS_KEY_ID=<key id>
BACKUP_S3_SECRET_ACCESS_KEY=<secret>
DATABASE_URL=postgres://tracker:<password>@<host>:5432/spotify_tracker?sslmode=disable
EOF

docker run --rm --env-file backup.env --network host --user "$(id -u)" \
  -v "$PWD/backup-age-key.txt:/run/secrets/backup-age-key:ro" \
  ghcr.io/baoopn/spotify-tracker-backup restore latest
```

`--network host` lets the container reach a database on `localhost` (Linux).
`--user` lets it read your key file. To get only the decrypted dump:

```bash
mkdir -p out
docker run --rm --env-file backup.env --user "$(id -u)" \
  -v "$PWD/backup-age-key.txt:/run/secrets/backup-age-key:ro" -v "$PWD/out:/restore" \
  ghcr.io/baoopn/spotify-tracker-backup restore latest --to-file /restore/spotify-tracker.dump
```

### (d) Without Docker

You need `rclone`, `age` and the PostgreSQL 17 (or newer) client tools.

```bash
# rclone settings from env: a remote called "r2"
export RCLONE_CONFIG_R2_TYPE=s3 RCLONE_CONFIG_R2_PROVIDER=Cloudflare \
  RCLONE_CONFIG_R2_REGION=auto RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true \
  RCLONE_CONFIG_R2_ENDPOINT=https://<account id>.r2.cloudflarestorage.com \
  RCLONE_CONFIG_R2_ACCESS_KEY_ID=<key id> RCLONE_CONFIG_R2_SECRET_ACCESS_KEY=<secret>

# 1. Pick a backup (newest last)
rclone lsf -R --include '*.dump.age' r2:<bucket>/spotify-tracker | sort | tail -n 5
NAME=spotify-tracker-20261109T030000Z.dump.age
DIR=r2:<bucket>/spotify-tracker/2026/11

# 2. Download the file and its manifest
rclone copyto "$DIR/$NAME" "$NAME"
rclone copyto "$DIR/$NAME.json" "$NAME.json"

# 3. Check the sha256 (macOS: shasum -a 256): both values must match
grep '"sha256"' "$NAME.json"
sha256sum "$NAME"

# 4. Decrypt straight into pg_restore (no plaintext file); count the tables
age -d -i backup-age-key.txt "$NAME" | pg_restore --list | grep -c ' TABLE '

# 5. Restore into an empty database. Single transaction: any failure,
#    including a decryption error, changes nothing.
age -d -i backup-age-key.txt "$NAME" | \
  pg_restore --no-owner --no-privileges --exit-on-error --single-transaction \
  -d 'postgres://tracker:<password>@localhost:5432/spotify_tracker'
```

## All settings

| Variable | Default | Meaning |
|---|---|---|
| `BACKUP_AGE_RECIPIENT` | | age public key(s), space or comma separated. Required. |
| `BACKUP_AGE_RECIPIENTS_FILE` | | file with one recipient per line (instead of, or as well as, the above) |
| `BACKUP_AGE_IDENTITY_FILE` | `/run/secrets/backup-age-key` | private key for `restore` / `verify` |
| `BACKUP_S3_ENDPOINT` | | S3 endpoint URL |
| `BACKUP_S3_BUCKET` | | bucket |
| `BACKUP_S3_ACCESS_KEY_ID` | | access key ID |
| `BACKUP_S3_SECRET_ACCESS_KEY` | | secret access key |
| `BACKUP_S3_REGION` | `auto` | region |
| `BACKUP_S3_PROVIDER` | `Cloudflare` | rclone S3 provider name |
| `BACKUP_REMOTE` | | any rclone path; wins over `BACKUP_S3_*` |
| `BACKUP_PREFIX` | `spotify-tracker` | key prefix in the bucket |
| `BACKUP_TIME` | `03:00` | daily run time, UTC |
| `BACKUP_ON_START` | `false` | also back up when the container starts |
| `BACKUP_RETRY_MINUTES` | `30` | delay before the one retry; `0` disables it |
| `BACKUP_KEEP_DAYS` | `30` | delete older backups; `0` keeps all |
| `BACKUP_KEEP_MIN` | `7` | always keep this many newest backups (at least 1) |
| `BACKUP_MAX_AGE_HOURS` | `26` | healthcheck limit |
| `BACKUP_PING_URL` | | healthchecks.io-style ping URL |
| `BACKUP_LOOP_SECONDS` | `60` | how often the scheduler wakes (heartbeat, settings, requests); the test sets 2 |
| `DATABASE_URL` or `PG*` | | the database to back up or restore into. A URL is split into `PG*` values, so the password never shows in a process list; query parameters other than `sslmode`, `sslrootcert`, `sslcert`, `sslkey`, `connect_timeout`, `application_name`, `options` and `target_session_attrs` are refused. Percent-encode special characters in the password. |

The compose files set the `PG*` variables for the bundled `postgres` service.

## Testing

`deploy/backup/test/roundtrip.sh` builds the image and runs a full backup and
restore with throwaway Postgres containers and a local Docker volume as the
remote (no cloud access). It compares the row count of every table and the
goose version, and checks the failure cases: no recipient, wrong key, a
changed file, a non-empty target, and the retention rules. It also checks
the status tables: run rows, the inventory against `backupctl list`, the
heartbeat, a queued request, the admin settings, a failing status write,
a stop during a backup, the clean-up after a restore, and status tables
that appear later. A failing status write must not stop the backup. CI
runs it on every pull request.

```bash
sh deploy/backup/test/roundtrip.sh
```
