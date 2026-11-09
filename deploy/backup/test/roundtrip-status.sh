# shellcheck shell=sh
# Status tables for the admin dashboard (migration 00011). Sourced by
# roundtrip.sh after its own checks; uses its helpers and containers.
#
# "A && ok || bad" is safe here: ok and bad always succeed.
# shellcheck disable=SC2015

# q SQL: one query on the source database, unaligned tuples.
q() {
	docker exec "$SRC" psql -X -q -A -t -v ON_ERROR_STOP=1 -U tracker -d spotify_tracker -c "$1"
}

# wait_true DESC SQL [SECONDS]: polls until SQL returns t.
wait_true() {
	i=0
	until [ "$(q "$2" 2>/dev/null)" = t ]; do
		i=$((i + 1))
		if [ "$i" -ge "${3:-60}" ]; then
			bad "$1 (timed out)"
			return 1
		fi
		sleep 1
	done
	ok "$1"
}

# loop_run NAME ARGS...: the scheduler against the source, 2 s loop.
loop_run() {
	n=$1
	shift
	docker run -d --name "$n" --network "$NET" -v "$VOL:/backups" \
		-e PGHOST="$SRC" -e PGUSER=tracker -e PGPASSWORD="$PGPW" -e PGDATABASE=spotify_tracker \
		-e BACKUP_LOOP_SECONDS=2 -e BACKUP_TIME=03:00 "$@" "$IMAGE" run >/dev/null
}

echo "== status tables"
r=$(q "SELECT count(*) FROM backup_runs WHERE trigger = 'manual' AND status = 'success' AND requested_by IS NULL
	AND name LIKE 'spotify-tracker-%.dump.age' AND sha256 ~ '^[0-9a-f]{64}\$' AND size_bytes > 0 AND tables > 0
	AND finished_at >= started_at")
[ "${r:-0}" -ge 1 ] && ok "backupctl backup records success runs ($r)" || bad "no success run row"
q "SELECT error FROM backup_runs WHERE status = 'failed' ORDER BY id" >"$TMP/errors"
grep -q "never stored unencrypted" "$TMP/errors" && grep -q "invalid age recipient" "$TMP/errors" &&
	ok "refused backups are recorded as failed with the reason" || bad "failed runs: $(cat "$TMP/errors")"
r=$(q "SELECT count(*) FROM backup_runs WHERE status = 'running'")
[ "$r" = 0 ] && ok "no run is left running" || bad "$r runs left running"
r=$(q "SELECT count(*) FROM backup_runs WHERE trigger = 'start' AND status = 'success'")
[ "${r:-0}" -ge 1 ] && ok "BACKUP_ON_START run recorded with trigger start" || bad "no start run"

# A failing status write (here: a trigger that rejects every write) must
# only log a WARN; the backup itself succeeds.
q "CREATE FUNCTION rt_fail() RETURNS trigger LANGUAGE plpgsql AS \$\$ BEGIN RAISE EXCEPTION 'rt status down'; END \$\$;
	CREATE TRIGGER rt_fail_runs BEFORE INSERT OR UPDATE ON backup_runs FOR EACH ROW EXECUTE FUNCTION rt_fail();
	CREATE TRIGGER rt_fail_inv BEFORE INSERT OR DELETE ON backup_inventory FOR EACH ROW EXECUTE FUNCTION rt_fail();" >/dev/null
sleep 1
if bk "$SRC" backup >"$TMP/name" 2>"$TMP/out"; then
	grep -q "WARN.*status: could not" "$TMP/out" && ok "a failing status write is a WARN; the backup succeeds" ||
		bad "no status WARN: $(cat "$TMP/out")"
else
	bad "backup failed because of a status write"
	cat "$TMP/out"
fi
q "DROP TRIGGER rt_fail_runs ON backup_runs; DROP TRIGGER rt_fail_inv ON backup_inventory; DROP FUNCTION rt_fail();" >/dev/null

echo "== partial config heartbeat"
loop_run "$ID-mis" -e BACKUP_REMOTE=/backups
wait_true "partial config: heartbeat with configured=false and the reason" \
	"SELECT configured = false AND config_error LIKE '%BACKUP_AGE_RECIPIENT%' AND last_seen_at > now() - interval '1 minute'
	 AND destination = 'remote:local/spotify-tracker' FROM backup_agent"
q "INSERT INTO backup_requests (kind, requested_by) VALUES ('backup', 'admin@example.com')" >/dev/null
wait_true "partial config: a request is answered with an error" \
	"SELECT bool_and(done_at IS NOT NULL AND error LIKE 'backups are not configured%') FROM backup_requests"
docker rm -f "$ID-mis" >/dev/null

echo "== scheduler loop"
# Leftovers of a killed scheduler (old: cleaned; fresh: left alone, an old
# container may still be at work), and rows past retention.
q "INSERT INTO backup_runs (trigger, status, started_at) VALUES ('schedule', 'running', now() - interval '3 hours'),
	('schedule', 'running', now() - interval '30 minutes'), ('schedule', 'success', now() - interval '200 days');
	INSERT INTO backup_requests (kind, requested_at, claimed_at) VALUES ('backup', now() - interval '3 hours', now() - interval '3 hours'),
		('refresh', now() - interval '10 minutes', now() - interval '10 minutes');
	INSERT INTO backup_requests (kind, requested_at, claimed_at, done_at)
		VALUES ('refresh', now() - interval '40 days', now() - interval '40 days', now() - interval '40 days');
	INSERT INTO settings (key, value) VALUES ('backup', '{\"time\":\"04:30\",\"paused\":true,\"keep_days\":30,\"keep_min\":7}')" >/dev/null
loop_run "$ID-loop" -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_REMOTE=/backups -e BACKUP_VERSION=rt-test
wait_true "heartbeat with the admin settings (time 04:30, paused, no next run)" \
	"SELECT configured AND config_error IS NULL AND backup_time = '04:30' AND paused AND next_run_at IS NULL
	 AND settings_source = 'admin' AND keep_days = 30 AND keep_min = 7 AND version = 'rt-test'
	 AND recipients = ARRAY['$RECIPIENT'] AND destination = 'remote:local/spotify-tracker'
	 AND last_seen_at > now() - interval '1 minute' FROM backup_agent"
r=$(q "SELECT count(*) FILTER (WHERE status = 'failed' AND error = 'interrupted' AND finished_at = started_at
		AND started_at < now() - interval '2 hours')
	|| ' ' || count(*) FILTER (WHERE status = 'running' AND started_at > now() - interval '1 hour')
	|| ' ' || count(*) FILTER (WHERE started_at < now() - interval '180 days') FROM backup_runs")
[ "$r" = "1 1 0" ] && ok "startup: run running > 2 h marked interrupted, a fresh one kept, old runs deleted" ||
	bad "startup runs: $r"
r=$(q "SELECT count(*) FILTER (WHERE error = 'interrupted') || ' ' || count(*) FILTER (WHERE claimed_at IS NOT NULL AND done_at IS NULL)
	|| ' ' || count(*) FILTER (WHERE done_at < now() - interval '30 days') FROM backup_requests")
[ "$r" = "1 1 0" ] && ok "startup: request claimed > 2 h marked interrupted, a fresh one kept, old ones deleted" ||
	bad "startup requests: $r"
docker exec "$ID-loop" backupctl health | grep -q '^ok: paused' && ok "health ok while paused" ||
	bad "paused health: $(docker exec "$ID-loop" backupctl health)"

RID=$(q "INSERT INTO backup_requests (kind, requested_by) VALUES ('backup', 'admin@example.com') RETURNING id")
wait_true "a manual request is claimed and completed while paused" \
	"SELECT done_at IS NOT NULL AND claimed_at IS NOT NULL AND error IS NULL AND run_id IS NOT NULL FROM backup_requests WHERE id = $RID"
r=$(q "SELECT r.status || ' ' || r.trigger || ' ' || r.requested_by FROM backup_requests q JOIN backup_runs r ON r.id = q.run_id WHERE q.id = $RID")
[ "$r" = "success manual admin@example.com" ] && ok "the request's run: $r" || bad "request run: $r"
REQ_NAME=$(q "SELECT r.name FROM backup_requests q JOIN backup_runs r ON r.id = q.run_id WHERE q.id = $RID")
bk "$SRC" list 2>/dev/null | sed '1d' | cut -d' ' -f1 | sort >"$TMP/list.names"
q "SELECT name FROM backup_inventory ORDER BY name" | sort >"$TMP/inv.names"
if [ -s "$TMP/list.names" ] && cmp -s "$TMP/list.names" "$TMP/inv.names"; then
	ok "inventory matches backupctl list ($(wc -l <"$TMP/inv.names" | tr -d ' ') backups)"
else
	bad "inventory differs from the list"
	diff "$TMP/list.names" "$TMP/inv.names" || true
fi
r=$(q "SELECT count(*) FROM backup_inventory i JOIN backup_runs r ON r.name = i.name
	WHERE r.status = 'success' AND (i.sha256 IS DISTINCT FROM r.sha256 OR i.size_bytes <> r.size_bytes
	OR i.created_at <> to_timestamp(substring(i.name from '([0-9]{8}T[0-9]{6})Z'), 'YYYYMMDD\"T\"HH24MISS'))")
[ "$r" = 0 ] && ok "inventory size, sha256 and time match the runs" || bad "inventory mismatches: $r"

# A refresh request re-lists the remote: add a complete fake backup first.
docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c '
	d=/backups/spotify-tracker/2021/02; mkdir -p $d; n=spotify-tracker-20210203T040506Z.dump.age
	echo fake >$d/$n; printf "{\n  \"sha256\": \"%s\"\n}\n" "$(printf %064d 0)" >$d/$n.json'
RID=$(q "INSERT INTO backup_requests (kind) VALUES ('refresh') RETURNING id")
wait_true "a refresh request re-reads the listing and manifests" \
	"SELECT (SELECT done_at IS NOT NULL AND error IS NULL FROM backup_requests WHERE id = $RID)
	 AND EXISTS (SELECT 1 FROM backup_inventory WHERE name = 'spotify-tracker-20210203T040506Z.dump.age'
	   AND sha256 = repeat('0', 64) AND created_at = '2021-02-03T04:05:06Z')"

q "UPDATE settings SET value = '{\"time\":\"05:15\",\"paused\":false,\"keep_days\":30,\"keep_min\":7}' WHERE key = 'backup'" >/dev/null
wait_true "new admin settings apply within the loop (05:15, next run at 05:15 UTC)" \
	"SELECT backup_time = '05:15' AND NOT paused AND to_char(next_run_at AT TIME ZONE 'UTC', 'HH24:MI') = '05:15'
	 AND next_run_at > now() AND next_run_at <= now() + interval '1 day' FROM backup_agent"
q "UPDATE settings SET value = '{\"time\":\"25:00\",\"keep_min\":0}' WHERE key = 'backup'" >/dev/null
sleep 5
r=$(q "SELECT backup_time || ' ' || keep_min FROM backup_agent")
[ "$r" = "05:15 7" ] && docker logs "$ID-loop" 2>&1 | grep -q "WARN.*settings are invalid" &&
	ok "invalid admin settings: WARN, previous values kept" || bad "invalid settings: $r"
q "DELETE FROM settings WHERE key = 'backup'" >/dev/null
wait_true "without admin settings the env values apply" \
	"SELECT backup_time = '03:00' AND settings_source = 'env' FROM backup_agent"

docker logs "$ID-loop" >"$TMP/loop.log" 2>&1
rc=0
grep -q -F -e "$PGPW" -e "$PGPW_URL" -e "admin@example.com" "$TMP/loop.log" || rc=$?
[ "$rc" -eq 1 ] && ok "scheduler logs contain no password or admin email" || bad "loop logs leak (grep exit $rc)"
t0=$(date +%s)
docker stop "$ID-loop" >/dev/null
[ $(($(date +%s) - t0)) -lt 8 ] && ok "docker stop of the loop is quick" || bad "loop stop took too long"
docker rm -f "$ID-loop" >/dev/null

echo "== restore cleans up the status tables"
# The request's backup was taken while its run was running, its request
# claimed, and the heartbeat present.
if bk "$DST" restore "$REQ_NAME" --force -i - <"$TMP/key.txt" >"$TMP/out" 2>&1; then
	r=$(docker exec "$DST" psql -X -A -t -U tracker -d spotify_tracker -c "
		SELECT (SELECT count(*) FROM backup_runs WHERE status = 'running') || ' ' ||
			(SELECT count(*) FROM backup_runs WHERE error = 'interrupted (restored from backup)' AND finished_at = started_at) || ' ' ||
			(SELECT count(*) FROM backup_requests WHERE done_at IS NULL) || ' ' ||
			(SELECT count(*) FROM backup_requests WHERE error = 'interrupted (restored)') || ' ' ||
			(SELECT count(*) FROM backup_agent)")
	[ "$r" = "0 2 0 2 0" ] && ok "restore: open runs and requests closed, heartbeat removed" || bad "restore fix-up: $r"
else
	bad "restore of $REQ_NAME"
	cat "$TMP/out"
fi

echo "== stop during a backup"
loop_run "$ID-term" -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_REMOTE=/backups -e BACKUP_DEBUG_SLEEP=120
RID=$(q "INSERT INTO backup_requests (kind, requested_by) VALUES ('backup', 'term@example.com') RETURNING id")
wait_true "the request's backup is running" \
	"SELECT EXISTS (SELECT 1 FROM backup_runs WHERE status = 'running' AND requested_by = 'term@example.com')"
t0=$(date +%s)
docker stop "$ID-term" >/dev/null
[ $(($(date +%s) - t0)) -lt 8 ] && ok "docker stop during a backup is quick" || bad "stop during a backup took too long"
r=$(q "SELECT r.status || '|' || r.error || '|' || q.error || '|' || (q.done_at IS NOT NULL)
	FROM backup_requests q JOIN backup_runs r ON r.id = q.run_id WHERE q.id = $RID")
[ "$r" = "failed|interrupted (service stopped)|interrupted|true" ] &&
	ok "stop: run marked interrupted, request done with its run" || bad "stop: $r"
docker logs "$ID-term" 2>&1 | grep -q "abandoning the running backup" && ok "stop logs the abandoned backup" || bad "no stop log"
docker rm -f "$ID-term" >/dev/null

echo "== database errors and secrets"
loop_run "$ID-nodb" -e PGHOST=no-such-host -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_REMOTE=/backups
i=0
until docker logs "$ID-nodb" 2>&1 | grep -q "database unavailable"; do
	i=$((i + 1))
	[ "$i" -lt 30 ] || break
	sleep 1
done
docker logs "$ID-nodb" 2>&1 | grep -q "WARN.*database unavailable.*backups continue): ." &&
	ok "the database unavailable WARN names the reason" || bad "WARN: $(docker logs "$ID-nodb" 2>&1 | grep unavailable)"
docker rm -f "$ID-nodb" >/dev/null
# shellcheck disable=SC2016 # expanded in the container
r=$(docker run --rm -e RCLONE_CONFIG_R2_SECRET_ACCESS_KEY=rt-secret-1 -e RCLONE_CONFIG_R2_ENDPOINT=https://rt-host.example/x \
	-e PGPASSWORD=rt-secret-2 --entrypoint sh "$IMAGE" -c '
	. /usr/local/bin/backupctl-common.sh; . /usr/local/bin/backupctl-status.sh
	short_error "e rt-secret-1 rt-secret-2 rt-host.example $(printf "\033")ok"')
[ "$r" = "e *** *** *** ok" ] && ok "errors are scrubbed (rclone env secrets, endpoint host, password)" || bad "scrub: $r"

echo "== status tables created after the service started"
LATE="$ID-late"
start_pg "$LATE"
wait_pg "$LATE"
for f in "$ROOT"/migrations/*.sql; do
	case $f in *00011_*) continue ;; esac
	awk '/^-- \+goose Up/ { up = 1; next } /^-- \+goose Down/ { up = 0 } up' "$f"
done | psql_in "$LATE" >/dev/null
echo "CREATE TABLE rt_late (id int); INSERT INTO rt_late VALUES (1);" | psql_in "$LATE" >/dev/null
docker run -d --name "$ID-laterun" --network "$NET" -v "$VOL:/backups" \
	-e PGHOST="$LATE" -e PGUSER=tracker -e PGPASSWORD="$PGPW" -e PGDATABASE=spotify_tracker \
	-e BACKUP_LOOP_SECONDS=2 -e BACKUP_PREFIX=late -e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT="$RECIPIENT" \
	-e BACKUP_ON_START=true "$IMAGE" run >/dev/null
i=0
until docker exec "$ID-laterun" test -f /tmp/backup-status/last_success 2>/dev/null; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || break
	sleep 1
done
[ "$i" -lt 60 ] && ok "a backup without status tables succeeds" || bad "the late backup did not finish"
awk '/^-- \+goose Up/ { up = 1; next } /^-- \+goose Down/ { up = 0 } up' "$ROOT"/migrations/00011_*.sql |
	psql_in "$LATE" >/dev/null
i=0
until [ "$(docker exec "$LATE" psql -X -A -t -U tracker -d spotify_tracker -c "
	SELECT EXISTS (SELECT 1 FROM backup_runs r JOIN backup_inventory i ON i.name = r.name
		WHERE r.trigger = 'start' AND r.status = 'success' AND r.sha256 = i.sha256 AND r.tables > 0)
	AND (SELECT count(*) FROM backup_inventory) = 1 AND EXISTS (SELECT 1 FROM backup_agent WHERE configured)" 2>/dev/null)" = t ]; do
	i=$((i + 1))
	[ "$i" -lt 30 ] || break
	sleep 1
done
[ "$i" -lt 30 ] && ok "tables appear: the earlier success is recorded and the inventory filled" ||
	bad "late tables: run or inventory missing"
docker rm -f "$ID-laterun" "$LATE" >/dev/null
