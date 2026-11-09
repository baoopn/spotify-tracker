#!/bin/sh
# End-to-end test for the backup image, with plain Docker and throwaway
# containers only (no published ports, no cloud storage):
#
#   sh deploy/backup/test/roundtrip.sh
#
# Starts two postgres:17-alpine containers on a private network, loads the
# repo's migrations plus seed data into the first, backs it up to a local
# rclone remote (a Docker volume) with a generated test key, restores into the
# second and compares row counts of every table and the goose version. Also
# checks: no-recipient refusal, wrong identity, tampered ciphertext, non-empty
# target refusal (and --force), --to-file, prune with BACKUP_KEEP_MIN, the
# idle and misconfigured modes, the scheduler's healthcheck, an empty database,
# a mounted key file, DATABASE_URL with an encoded password, and a prune
# that must stop when the listing fails.
#
# Env: BACKUP_TEST_IMAGE (default spotify-tracker-backup:test), SKIP_BUILD=1
# to reuse an existing image, KEEP_ON_FAIL=1 to leave the containers.
#
# "A && ok || bad" is safe here: ok and bad always succeed.
# shellcheck disable=SC2015
set -eu
export MSYS_NO_PATHCONV=1 # Git Bash: don't rewrite /paths in docker args

# pwd -W: Windows form of the path for docker (Git Bash); plain pwd elsewhere.
ROOT=$(cd "$(dirname "$0")/../../.." && { pwd -W 2>/dev/null || pwd; })
IMAGE=${BACKUP_TEST_IMAGE:-spotify-tracker-backup:test}
ID="strt-$$-$(date +%s)"
NET="$ID-net"
VOL="$ID-remote"
SRC="$ID-src"
DST="$ID-dst"
TMP=$(mktemp -d)
KEYVOL="$ID-key"
# A password that needs URL encoding (DATABASE_URL coverage).
PGPW="rt p@ss:w/rd"
PGPW_URL="rt%20p%40ss%3Aw%2Frd"
PASS=0
FAILED=0

cleanup() {
	rc=$?
	if [ "$rc" -ne 0 ] && [ "${KEEP_ON_FAIL:-}" = 1 ]; then
		echo "KEEP_ON_FAIL: leaving $SRC $DST $NET $VOL"
	else
		docker rm -f "$SRC" "$DST" "$ID-run" "$ID-idle" "$ID-bad1" "$ID-bad2" >/dev/null 2>&1 || true
		docker network rm "$NET" >/dev/null 2>&1 || true
		docker volume rm "$VOL" "$KEYVOL" >/dev/null 2>&1 || true
	fi
	rm -rf "$TMP"
	exit "$rc"
}
trap cleanup EXIT INT TERM

ok() {
	PASS=$((PASS + 1))
	echo "  ok   $*"
}
bad() {
	FAILED=$((FAILED + 1))
	echo "  FAIL $*"
}

# bk HOST ARGS...: runs the backup image against database HOST.
bk() {
	host=$1
	shift
	docker run --rm -i --network "$NET" -v "$VOL:/backups" \
		-e PGHOST="$host" -e PGUSER=tracker -e PGPASSWORD="$PGPW" -e PGDATABASE="${DBNAME:-spotify_tracker}" \
		-e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT="${RECIPIENT:-}" \
		-e BACKUP_KEEP_DAYS="${KEEP_DAYS:-30}" -e BACKUP_KEEP_MIN="${KEEP_MIN:-7}" \
		-e BACKUP_DEBUG_WORKDIR=1 \
		"$IMAGE" "$@"
}

# with_key FILE CMD...: runs CMD with FILE on stdin.
with_key() {
	f=$1
	shift
	"$@" <"$f"
}

# bk_url HOST ARGS...: like bk, but connects with DATABASE_URL.
bk_url() {
	host=$1
	shift
	docker run --rm -i --network "$NET" -v "$VOL:/backups" -e BACKUP_REMOTE=/backups \
		-e DATABASE_URL="postgres://tracker:$PGPW_URL@$host:5432/spotify_tracker?sslmode=disable" \
		-e BACKUP_DEBUG_WORKDIR=1 \
		"$IMAGE" "$@"
}

# check_workdir DESC FILE: FILE holds backupctl output with BACKUP_DEBUG_WORKDIR=1.
# Only ciphertext, manifest, error logs, the recipient list and the TOC
# counters may have been left in the work dir: never a plaintext dump.
check_workdir() {
	files=$(sed -n 's/.*workdir file: \([^ ]*\) .*/\1/p' "$2")
	other=$(printf '%s\n' "$files" | grep -v -E '\.dump\.age(\.json)?$|\.err$|^recipients$|^toc\.result$' || true)
	if [ -z "$files" ]; then
		bad "$1: no work dir listing"
	elif [ -n "$other" ]; then
		bad "$1: unexpected files in the work dir: $other"
	else
		ok "$1: only ciphertext and logs in the work dir"
	fi
}

# expect_fail DESC PATTERN CMD...: CMD must fail and print PATTERN.
expect_fail() {
	desc=$1
	pattern=$2
	shift 2
	if "$@" >"$TMP/out" 2>&1; then
		bad "$desc: command succeeded"
		sed 's/^/       /' "$TMP/out"
	elif grep -q -- "$pattern" "$TMP/out"; then
		ok "$desc"
	else
		bad "$desc: expected '$pattern' in output"
		sed 's/^/       /' "$TMP/out"
	fi
}

psql_in() {
	c=$1
	shift
	docker exec -i "$c" psql -X -q -v ON_ERROR_STOP=1 -U tracker -d spotify_tracker "$@"
}

# counts CONTAINER: "table rows" for every base table in schema public.
counts() {
	docker exec "$1" psql -X -A -t -F ' ' -U tracker -d spotify_tracker -c "
		SELECT table_name, (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I',
			table_schema, table_name), false, true, '')))[1]::text
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY 1"
	docker exec "$1" psql -X -A -t -U tracker -d spotify_tracker \
		-c "SELECT 'goose_max ' || max(version_id) FROM goose_db_version WHERE is_applied"
}

start_pg() {
	docker run -d --name "$1" --network "$NET" -e POSTGRES_USER=tracker \
		-e POSTGRES_PASSWORD="$PGPW" -e POSTGRES_DB=spotify_tracker postgres:17-alpine >/dev/null
}

wait_pg() {
	i=0
	until docker exec "$1" pg_isready -U tracker -d spotify_tracker -h 127.0.0.1 >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt 60 ] || {
			echo "postgres $1 did not start"
			exit 1
		}
		sleep 1
	done
}

echo "== setup"
if [ "${SKIP_BUILD:-}" != 1 ]; then
	docker build -q -f "$ROOT/deploy/backup/Dockerfile" -t "$IMAGE" "$ROOT" >/dev/null
fi
docker network create --internal "$NET" >/dev/null
docker volume create "$VOL" >/dev/null
# The image runs as uid 10001; a fresh volume is root-owned when mounted
# somewhere the image has no directory, so prepare it explicitly.
docker run --rm -u 0 -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c 'chown 10001:10001 /backups'
start_pg "$SRC"
start_pg "$DST"
wait_pg "$SRC"
wait_pg "$DST"

docker run --rm "$IMAGE" keygen >"$TMP/key.txt" 2>"$TMP/keygen.err"
docker run --rm "$IMAGE" keygen >"$TMP/wrong.txt" 2>/dev/null
RECIPIENT=$(sed -n 's/^# public key: //p' "$TMP/key.txt")
case $RECIPIENT in age1*) ok "keygen prints an identity and its recipient" ;; *) bad "keygen" ;; esac
grep -q "Store it safely" "$TMP/keygen.err" && ok "keygen warns on stderr" || bad "keygen stderr note"

# Schema: the Up part of every goose migration, then goose's own table.
for f in "$ROOT"/migrations/*.sql; do
	awk '/^-- \+goose Up/ { up = 1; next } /^-- \+goose Down/ { up = 0 } up' "$f"
done | psql_in "$SRC" >/dev/null
versions=
for f in "$ROOT"/migrations/*.sql; do
	v=${f##*/}
	v=${v%%_*}
	versions="$versions $(printf '%s' "$v" | sed 's/^0*//')"
done
{
	echo "CREATE TABLE goose_db_version (id serial PRIMARY KEY, version_id bigint NOT NULL,"
	echo "  is_applied boolean NOT NULL, tstamp timestamp DEFAULT now());"
	echo "INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, true);"
	for v in $versions; do echo "INSERT INTO goose_db_version (version_id, is_applied) VALUES ($v, true);"; done
	echo "CREATE TABLE rt_seed (id bigserial PRIMARY KEY, label text NOT NULL, payload jsonb, at timestamptz DEFAULT now());"
	echo "INSERT INTO rt_seed (label, payload) SELECT 'row ' || g || ' café ü', jsonb_build_object('n', g) FROM generate_series(1, 5000) g;"
} | psql_in "$SRC" >/dev/null
counts "$SRC" >"$TMP/src.counts"
echo "   source: $(wc -l <"$TMP/src.counts" | tr -d ' ') tables, $(grep goose_max "$TMP/src.counts")"

echo "== safety"
RECIPIENT_SAVE=$RECIPIENT
RECIPIENT=
expect_fail "backup without a recipient is refused" "never stored unencrypted" bk "$SRC" backup
RECIPIENT=$RECIPIENT_SAVE
n=$(docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c 'find /backups -type f | wc -l')
[ "$n" -eq 0 ] && ok "nothing was uploaded without a recipient" || bad "files found after refusal: $n"
RECIPIENT="not-a-key"
expect_fail "invalid recipient is refused" "invalid age recipient" bk "$SRC" backup
RECIPIENT=$RECIPIENT_SAVE
psql_in "$SRC" -c "CREATE DATABASE rt_empty" >/dev/null
DBNAME=rt_empty
expect_fail "backup of a database without tables is refused" "contains no tables" bk "$SRC" backup
DBNAME=
n=$(docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c 'find /backups -type f | wc -l')
[ "$n" -eq 0 ] && ok "nothing was uploaded for the empty database" || bad "files found after empty dump: $n"

echo "== backup"
if bk "$SRC" backup >"$TMP/name" 2>"$TMP/out"; then ok "backup"; else
	bad "backup"
	cat "$TMP/out"
	exit 1
fi
NAME=$(tail -n 1 "$TMP/name")
check_workdir "backup" "$TMP/out"
bk "$SRC" list >"$TMP/list" 2>&1
grep -q "$NAME" "$TMP/list" && ok "list shows $NAME" || bad "list: $(cat "$TMP/list")"
docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c "cat /backups/spotify-tracker/*/*/$NAME.json" >"$TMP/manifest"
grep -q '"format": "pg_dump-custom+age"' "$TMP/manifest" && grep -q '"goose_version": [0-9]' "$TMP/manifest" &&
	ok "manifest has format and goose version" || bad "manifest: $(cat "$TMP/manifest")"
if docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c \
	"head -c 64 /backups/spotify-tracker/*/*/$NAME | grep -q age-encryption.org"; then
	ok "object is age ciphertext"
else bad "object is not age ciphertext"; fi

echo "== verify / restore"
if bk "$SRC" verify latest -i - <"$TMP/key.txt" >"$TMP/out" 2>&1; then ok "verify latest"; else
	bad "verify"
	cat "$TMP/out"
fi
check_workdir "verify" "$TMP/out"
docker volume create "$KEYVOL" >/dev/null
docker run --rm -i -u 0 -v "$KEYVOL:/k" --entrypoint sh "$IMAGE" -c \
	'cat >/k/backup-age-key && chown 10001 /k/backup-age-key && chmod 400 /k/backup-age-key' <"$TMP/key.txt"
if docker run --rm -v "$VOL:/backups" -v "$KEYVOL:/run/secrets:ro" -e BACKUP_REMOTE=/backups \
	"$IMAGE" verify latest >"$TMP/out" 2>&1; then
	ok "verify with the key file at /run/secrets/backup-age-key"
else
	bad "verify with a mounted key"
	cat "$TMP/out"
fi
expect_fail "verify with the wrong identity fails" "decryption failed" \
	with_key "$TMP/wrong.txt" bk "" verify latest -i -
expect_fail "restore without an identity fails" "identity not readable" bk "$DST" restore latest

if bk "$DST" restore latest -i - <"$TMP/key.txt" >"$TMP/out" 2>&1; then ok "restore into an empty database"; else
	bad "restore"
	cat "$TMP/out"
fi
check_workdir "restore" "$TMP/out"
counts "$DST" >"$TMP/dst.counts"
if cmp -s "$TMP/src.counts" "$TMP/dst.counts"; then
	ok "row counts match for $(wc -l <"$TMP/src.counts" | tr -d ' ') tables + goose version"
else
	bad "row counts differ"
	diff "$TMP/src.counts" "$TMP/dst.counts" || true
fi

expect_fail "restore into a non-empty database is refused" "refusing to restore" \
	with_key "$TMP/key.txt" bk_url "$DST" restore latest -i -
psql_in "$DST" -c "DELETE FROM rt_seed WHERE id <= 100" >/dev/null
if bk_url "$DST" restore "$NAME" --force -i - <"$TMP/key.txt" >"$TMP/out" 2>&1; then
	ok "restore --force by name (DATABASE_URL, URL-encoded password)"
else
	bad "restore --force"
	cat "$TMP/out"
fi
check_workdir "restore --force" "$TMP/out"
counts "$DST" >"$TMP/dst.counts"
cmp -s "$TMP/src.counts" "$TMP/dst.counts" && ok "row counts match after --force" || bad "row counts differ after --force"

if docker run --rm -i -v "$VOL:/backups" -e BACKUP_REMOTE=/backups --entrypoint sh "$IMAGE" -c \
	'backupctl restore latest --to-file /tmp/out.dump -i - && pg_restore --list /tmp/out.dump | grep -q "TABLE DATA public rt_seed"' \
	<"$TMP/key.txt" >"$TMP/out" 2>&1; then
	ok "restore --to-file writes a pg_restore-able dump (no database)"
else
	bad "--to-file"
	cat "$TMP/out"
fi

echo "== scheduler"
docker run -d --name "$ID-idle" "$IMAGE" run >/dev/null
# Partial settings: a destination without a recipient, a recipient with 1 of 4 S3 values.
docker run -d --name "$ID-bad1" -e BACKUP_REMOTE=/backups "$IMAGE" run >/dev/null
docker run -d --name "$ID-bad2" -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_S3_BUCKET=x "$IMAGE" run >/dev/null
sleep 2
for c in bad1 bad2; do
	if docker exec "$ID-$c" backupctl health >"$TMP/out" 2>&1; then
		bad "$c: partial config reported healthy: $(cat "$TMP/out")"
	elif grep -q misconfigured "$TMP/out" && docker logs "$ID-$c" 2>&1 | grep -q "ERROR.*misconfigured"; then
		ok "$c: partial config logs an ERROR and is unhealthy"
	else
		bad "$c: $(cat "$TMP/out")"
	fi
done
docker rm -f "$ID-bad1" "$ID-bad2" >/dev/null
docker exec "$ID-idle" backupctl health | grep -q '^idle' && ok "unconfigured service idles and is healthy" || bad "idle health"
docker logs "$ID-idle" 2>&1 | grep -q "WARN.*disabled" && ok "idle logs a WARN" || bad "idle WARN"
t0=$(date +%s)
docker stop "$ID-idle" >/dev/null
[ $(($(date +%s) - t0)) -lt 8 ] && ok "docker stop is quick (SIGTERM handled)" || bad "docker stop took too long"

sleep 1 # next backup gets a new name
docker run -d --name "$ID-run" --network "$NET" -v "$VOL:/backups" \
	-e PGHOST="$SRC" -e PGUSER=tracker -e PGPASSWORD="$PGPW" -e PGDATABASE=spotify_tracker \
	-e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_ON_START=true "$IMAGE" run >/dev/null
i=0
until docker exec "$ID-run" test -f /tmp/backup-status/last_success 2>/dev/null; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || break
	sleep 1
done
docker exec "$ID-run" backupctl health | grep -q '^ok: last backup' && ok "BACKUP_ON_START backs up; health ok" ||
	bad "scheduler health: $(docker exec "$ID-run" backupctl health)"
docker logs "$ID-run" 2>&1 | grep -q "next backup at" && ok "scheduler waits for BACKUP_TIME" || bad "no next-run log"
docker logs "$ID-run" >"$TMP/run.log" 2>&1
rc=0
grep -q -F -e "$RECIPIENT" -e "$PGPW" -e "$PGPW_URL" "$TMP/run.log" || rc=$?
[ "$rc" -eq 1 ] && ok "logs contain no recipient or password" || bad "logs leak config values (grep exit $rc)"
docker rm -f "$ID-run" >/dev/null
NAME2=$(bk "$SRC" list 2>/dev/null | sed -n '2p' | cut -d' ' -f1)
[ -n "$NAME2" ] && [ "$NAME2" != "$NAME" ] && ok "second backup $NAME2" || bad "second backup missing"

echo "== tamper"
docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c \
	"f=\$(ls /backups/spotify-tracker/*/*/$NAME2); printf 'X' | dd of=\"\$f\" bs=1 seek=200 conv=notrunc 2>/dev/null"
expect_fail "tampered ciphertext fails the sha256 check" "sha256 mismatch" \
	with_key "$TMP/key.txt" bk "" verify "$NAME2" -i -

echo "== prune"
# Ten complete, old (2020) backups plus one old orphan dump without manifest.
docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c '
	d=/backups/spotify-tracker/2020/01; mkdir -p $d
	for day in 01 02 03 04 05 06 07 08 09 10; do
		n=spotify-tracker-202001${day}T030000Z.dump.age
		echo fake >$d/$n; echo "{}" >$d/$n.json
	done
	mkdir -p /backups/spotify-tracker/2019/12
	echo orphan >/backups/spotify-tracker/2019/12/spotify-tracker-20191231T030000Z.dump.age'
KEEP_DAYS=0 bk "$SRC" prune >/dev/null 2>&1
count_objects() {
	docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c 'find /backups -name "*.dump.age" | wc -l'
}
[ "$(count_objects)" -eq 13 ] && ok "BACKUP_KEEP_DAYS=0 keeps everything" || bad "KEEP_DAYS=0 deleted: $(count_objects)"
# A listing error (unreadable directory) must stop the prune before it deletes anything.
docker run --rm -u 0 -v "$VOL:/backups" --entrypoint chmod "$IMAGE" 000 /backups/spotify-tracker/2020
KEEP_DAYS=30
KEEP_MIN=7
expect_fail "prune stops when the listing fails" "nothing deleted" bk "$SRC" prune
docker run --rm -u 0 -v "$VOL:/backups" --entrypoint chmod "$IMAGE" 755 /backups/spotify-tracker/2020
[ "$(count_objects)" -eq 13 ] && ok "nothing was deleted after the listing failure" || bad "listing failure deleted: $(count_objects)"
KEEP_DAYS=30 KEEP_MIN=7 bk "$SRC" prune >"$TMP/out" 2>&1 || bad "prune exited non-zero"
left=$(docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c 'find /backups -name "*.dump.age" | sed "s#.*/##" | sort')
n=$(printf '%s\n' "$left" | wc -l | tr -d ' ')
if [ "$n" -eq 7 ] && printf '%s\n' "$left" | grep -q "$NAME" &&
	printf '%s\n' "$left" | grep -q 20200106 && ! printf '%s\n' "$left" | grep -q -e 20200105 -e 20191231; then
	ok "prune keeps the newest KEEP_MIN=7 (2 new + 5 old), drops older ones and the orphan"
else
	bad "prune left $n: $left"
	cat "$TMP/out"
fi
jsons=$(docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c 'find /backups -name "*.json" | wc -l')
[ "$jsons" -eq 7 ] && ok "prune removes manifests with their dumps" || bad "manifests left: $jsons"

echo
echo "roundtrip: $PASS passed, $FAILED failed"
[ "$FAILED" -eq 0 ]
