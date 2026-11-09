# shellcheck shell=sh
# Globals are shared with the other sourced files (backupctl sources all).
# shellcheck disable=SC2034,SC2153
# Backup status in the application database (migration 00011): runs,
# inventory, the agent heartbeat, admin requests and the admin settings.
# Sourced by backupctl.
#
# Everything here is best-effort: a failed status write logs a WARN and
# returns non-zero, but never fails or skips a backup. Values reach SQL only
# as psql variables (:'name'), never by string interpolation. Nothing secret
# is written: no storage keys, no endpoint, no private key.

STATUS_DB=0   # 1 when the database is reachable and has the status tables
STATUS_DOWN=  # non-empty while the database is unreachable (log once)
LOOP_SECONDS=${BACKUP_LOOP_SECONDS:-60}

# db_usable: true when PG* settings are complete (setup_db would succeed).
# Quiet; never exits.
db_usable() {
	(setup_db) >/dev/null 2>&1 || return 1
	setup_db
}

# dbq SQL [NAME=VALUE...]: runs SQL with psql variables NAME. SQL is fed on
# stdin, because psql interpolates :'NAME' only there (not with -c). Prints
# unaligned tuples, fields separated by "|". Errors go to $STATUS_ERR.
dbq() {
	_q=$1
	shift
	_n=$#
	while [ "$_n" -gt 0 ]; do
		set -- "$@" -v "$1"
		shift
		_n=$((_n - 1))
	done
	status_err_init
	printf '%s\n' "SET statement_timeout = '${STATUS_TIMEOUT:-30s}';" "SET TIME ZONE 'UTC';" "$_q" |
		PGCONNECT_TIMEOUT=${PGCONNECT_TIMEOUT:-5} psql -X -q -A -t -F '|' -v ON_ERROR_STOP=1 "$@" 2>"$STATUS_ERR"
}

# status_err_init: picks the file for psql errors. Call it in the parent
# shell before a $(dbq ...), so db_error there reads the same file.
status_err_init() {
	[ -z "${STATUS_ERR:-}" ] || return 0
	STATUS_ERR=/dev/null
	if mkdir -p "$STATUS_DIR" 2>/dev/null && : 2>/dev/null >"$STATUS_DIR/db.err"; then
		STATUS_ERR=$STATUS_DIR/db.err
	fi
}

# db_error: first line of the last psql error, ASCII only, short.
db_error() {
	head -n 1 "$STATUS_ERR" 2>/dev/null | scrub | cut -c1-200
}

# status_check: once per loop. Sets STATUS_DB=1 when the database answers and
# has every status table (an older database without migration 00011 is
# skipped silently). Logs once when it goes down and when it comes back.
status_check() {
	STATUS_DB=0
	[ -n "${DB_NAME:-}" ] || return 1
	status_err_init
	if ! r=$(dbq "SELECT to_regclass('public.backup_runs') IS NOT NULL AND to_regclass('public.backup_inventory') IS NOT NULL
		AND to_regclass('public.backup_agent') IS NOT NULL AND to_regclass('public.backup_requests') IS NOT NULL
		AND to_regclass('public.settings') IS NOT NULL"); then
		[ -n "$STATUS_DOWN" ] || warn "status: database unavailable, status is not recorded (backups continue): $(db_error)"
		STATUS_DOWN=1
		return 1
	fi
	[ -z "$STATUS_DOWN" ] || info "status: database available again"
	STATUS_DOWN=
	[ "$r" = t ] || return 1
	STATUS_DB=1
}

# status_sql DESC SQL [NAME=VALUE...]: dbq when the status tables exist;
# a failure is a WARN. Prints the query output.
status_sql() {
	[ "$STATUS_DB" = 1 ] || return 1
	_d=$1
	shift
	if ! dbq "$@"; then
		warn "status: could not $_d (ignored): $(db_error)"
		return 1
	fi
}

# scrub: ASCII only, no control characters, and known secret values replaced
# by "***", so an error message is safe to store and show.
scrub() {
	tr -cd '\11\40-\176\n' | awk '
		BEGIN {
			n = split("BACKUP_S3_SECRET_ACCESS_KEY BACKUP_S3_ACCESS_KEY_ID BACKUP_S3_ENDPOINT PGPASSWORD DATABASE_URL BACKUP_PING_URL", v, " ")
			# rclone remotes configured from env (RCLONE_CONFIG_<NAME>_*).
			for (e in ENVIRON)
				if (e ~ /^RCLONE_CONFIG_.*(SECRET|KEY|ENDPOINT|PASS|TOKEN)/) v[++n] = e
			for (i = 1; i <= n; i++) {
				s = ENVIRON[v[i]]
				if (length(s) >= 4) sec[++k] = s
				if (v[i] ~ /ENDPOINT/ && s != "") {
					h = s; sub(/^[a-zA-Z]+:\/\//, "", h); sub(/[\/:].*/, "", h)
					if (length(h) >= 4) sec[++k] = h
				}
			}
		}
		{
			line = $0
			for (i = 1; i <= k; i++) {
				out = ""
				while ((p = index(line, sec[i])) > 0) {
					out = out substr(line, 1, p - 1) "***"
					line = substr(line, p + length(sec[i]))
				}
				line = out line
			}
			print line
		}'
}

# short_error TEXT: first line, scrubbed, at most 500 characters.
short_error() {
	printf '%s\n' "$1" | sed -e '/^[[:space:]]*$/d' | head -n 1 | scrub | cut -c1-500
}

# ─── Runs ─────────────────────────────────────────────────────────────────────

# run_start TRIGGER REQUESTED_BY: inserts a 'running' row, prints its id.
run_start() {
	status_sql "record the backup start" "INSERT INTO backup_runs (trigger, status, requested_by)
		VALUES (:'trigger', 'running', NULLIF(:'by', '')) RETURNING id" "trigger=$1" "by=$2"
}

# run_finish ID TRIGGER BY STARTED_EPOCH STATUS NAME SIZE SHA TABLES ERROR:
# updates the run row, or inserts a complete one when the start was not
# recorded (the database was down then). Prints the run id.
run_finish() {
	status_sql "record the backup result" "WITH upd AS (
		UPDATE backup_runs SET status = :'status', finished_at = now(), name = NULLIF(:'name', ''),
			size_bytes = NULLIF(:'size', '')::bigint, sha256 = NULLIF(:'sha', ''),
			tables = NULLIF(:'tables', '')::integer, error = left(NULLIF(:'err', ''), 500)
		WHERE id = NULLIF(:'id', '')::bigint RETURNING id
	), ins AS (
		INSERT INTO backup_runs (trigger, status, started_at, finished_at, name, size_bytes, sha256, tables, error, requested_by)
		SELECT :'trigger', :'status', to_timestamp(:'started'::bigint), now(), NULLIF(:'name', ''),
			NULLIF(:'size', '')::bigint, NULLIF(:'sha', ''), NULLIF(:'tables', '')::integer,
			left(NULLIF(:'err', ''), 500), NULLIF(:'by', '')
		WHERE NOT EXISTS (SELECT 1 FROM upd) RETURNING id
	) SELECT id FROM upd UNION ALL SELECT id FROM ins" \
		"id=$1" "trigger=$2" "by=$3" "started=$4" "status=$5" "name=$6" "size=$7" "sha=$8" "tables=$9" "err=${10}"
}

# status_startup: marks runs left 'running', and requests left claimed, by a
# previous scheduler for over 2 h as interrupted (fresh ones may belong to an
# old container still at work during a rolling deploy); applies retention.
status_startup() {
	status_sql "mark interrupted runs" "UPDATE backup_runs SET status = 'failed', finished_at = started_at,
		error = 'interrupted' WHERE status = 'running' AND started_at < now() - interval '2 hours'" >/dev/null
	status_sql "mark interrupted requests" "UPDATE backup_requests SET done_at = now(), error = 'interrupted'
		WHERE claimed_at IS NOT NULL AND done_at IS NULL AND claimed_at < now() - interval '2 hours'" >/dev/null
	status_retention
}

# status_stopped RUN_ID REQ_ID REQ_RUN_ID: on SIGTERM, marks the current run
# and the current request as interrupted. Short timeouts: docker stop waits
# only 10 s.
status_stopped() {
	[ -n "$1$2" ] || return 0
	STATUS_TIMEOUT=3s PGCONNECT_TIMEOUT=3 status_sql "mark the interrupted run" \
		"UPDATE backup_runs SET status = 'failed', finished_at = now(), error = 'interrupted (service stopped)'
			WHERE id = NULLIF(:'run', '')::bigint AND status = 'running';
		UPDATE backup_requests SET done_at = now(), error = 'interrupted',
			run_id = coalesce(run_id, NULLIF(:'reqrun', '')::bigint)
			WHERE id = NULLIF(:'req', '')::bigint AND done_at IS NULL" \
		"run=$1" "req=$2" "reqrun=$3" >/dev/null
}

# status_restored: after a restore, the target holds the status rows of the
# moment the backup was taken. Ends the runs and requests that were open
# then, and removes the old heartbeat (the live service writes a new one).
status_restored() {
	status_check || return 0
	status_sql "clean up the restored status tables" \
		"UPDATE backup_runs SET status = 'failed', finished_at = started_at,
			error = 'interrupted (restored from backup)' WHERE status = 'running';
		UPDATE backup_requests SET done_at = now(), error = 'interrupted (restored)' WHERE done_at IS NULL;
		DELETE FROM backup_agent" >/dev/null
}

# status_retention: runs older than 180 days, done requests older than 30.
status_retention() {
	status_sql "apply status retention" "DELETE FROM backup_runs WHERE started_at < now() - interval '180 days';
		DELETE FROM backup_requests WHERE done_at < now() - interval '30 days'" >/dev/null
}

# ─── Inventory ────────────────────────────────────────────────────────────────

# inventory_refresh: replaces backup_inventory with the remote listing of
# complete backups, in one transaction. The sha256 comes from earlier rows
# or runs; missing ones are read from the manifests (at most 50 per call).
# Skipped (non-zero) when the listing fails.
inventory_refresh() {
	[ "$STATUS_DB" = 1 ] || return 0
	if ! inv_listing=$(remote_listing); then
		warn "status: cannot list backups; inventory not refreshed"
		return 1
	fi
	inv_objs=$(complete_from "$inv_listing")
	inv_known=$(status_sql "read known checksums" "SELECT name || ';' || sha256 FROM backup_inventory WHERE sha256 IS NOT NULL
		UNION SELECT name || ';' || sha256 FROM backup_runs WHERE status = 'success' AND name IS NOT NULL AND sha256 IS NOT NULL") ||
		inv_known=
	inv_rows=
	inv_fetched=0
	inv_tmp=$(mktemp "${TMPDIR:-/tmp}/backupctl-inv.XXXXXX") || return 1
	while IFS=';' read -r n s _; do
		valid_name "$n" || continue
		is_uint "$s" || continue
		sha=$(printf '%s\n' "$inv_known" | awk -F';' -v n="$n" '$1 == n { print $2; exit }')
		if [ -z "$sha" ] && [ "$inv_fetched" -lt 50 ]; then
			inv_fetched=$((inv_fetched + 1))
			if rcl cat "$(object_path "$n").json" >"$inv_tmp" 2>/dev/null </dev/null; then
				sha=$(manifest_field "$inv_tmp" sha256)
			fi
		fi
		case $sha in *[!0-9a-f]* | '') sha= ;; esac
		inv_rows="$inv_rows$n;$s;$sha
"
	done <<EOF
$inv_objs
EOF
	rm -f "$inv_tmp"
	status_sql "refresh the backup inventory" "BEGIN;
		DELETE FROM backup_inventory;
		INSERT INTO backup_inventory (name, size_bytes, created_at, sha256)
		SELECT split_part(l, ';', 1), split_part(l, ';', 2)::bigint,
			to_timestamp(substring(split_part(l, ';', 1) from '([0-9]{8}T[0-9]{6})Z'), 'YYYYMMDD\"T\"HH24MISS'),
			NULLIF(split_part(l, ';', 3), '')
		FROM regexp_split_to_table(:'rows', chr(10)) AS l WHERE l <> '';
		COMMIT;" "rows=$inv_rows" >/dev/null
}

# ─── Admin settings (settings key 'backup', plain JSON) ───────────────────────

# settings_read: prints "time|keep_days|keep_min|paused|present" from the
# settings row; an absent field is empty, an invalid one is "!". Prints
# nothing when there is no row. Fails when the query fails.
settings_read() {
	status_sql "read the backup settings" "SELECT
		CASE WHEN jsonb_typeof(value) <> 'object' THEN '!|!|!|!' ELSE
		CASE WHEN jsonb_typeof(value->'time') IS NULL OR jsonb_typeof(value->'time') = 'null' OR value->>'time' = '' THEN ''
			WHEN jsonb_typeof(value->'time') = 'string' AND value->>'time' ~ '^([01][0-9]|2[0-3]):[0-5][0-9]\$' THEN value->>'time'
			ELSE '!' END || '|' ||
		CASE WHEN jsonb_typeof(value->'keep_days') IS NULL OR jsonb_typeof(value->'keep_days') = 'null' THEN ''
			WHEN jsonb_typeof(value->'keep_days') = 'number' AND value->>'keep_days' ~ '^[0-9]{1,4}\$' THEN value->>'keep_days'
			ELSE '!' END || '|' ||
		CASE WHEN jsonb_typeof(value->'keep_min') IS NULL OR jsonb_typeof(value->'keep_min') = 'null' THEN ''
			WHEN jsonb_typeof(value->'keep_min') = 'number' AND value->>'keep_min' ~ '^[0-9]{1,4}\$' THEN value->>'keep_min'
			ELSE '!' END || '|' ||
		CASE WHEN jsonb_typeof(value->'paused') IS NULL OR jsonb_typeof(value->'paused') = 'null' THEN ''
			WHEN jsonb_typeof(value->'paused') = 'boolean' THEN value->>'paused'
			ELSE '!' END END
		FROM settings WHERE key = 'backup'"
}

# settings_apply: sets EFF_TIME, EFF_KEEP_DAYS, EFF_KEEP_MIN, EFF_PAUSED and
# EFF_SOURCE from the admin settings, falling back to the env values. On an
# invalid document or a failed read, the previous values stay (WARN once
# per new document).
settings_apply() {
	[ "$STATUS_DB" = 1 ] || return 0
	raw=$(settings_read) || return 0
	t='' kd='' km='' p=''
	if [ -n "$raw" ]; then
		IFS='|' read -r t kd km p <<EOF
$raw
EOF
		bad=
		case "|$t|$kd|$km|$p|" in *'|!|'*) bad="wrong type or format" ;; esac
		if [ -z "$bad" ] && [ -n "$kd" ] && [ "$kd" -gt 3650 ]; then bad="keep_days must be 0..3650"; fi
		if [ -z "$bad" ] && [ -n "$km" ] && { [ "$km" -lt 1 ] || [ "$km" -gt 1000 ]; }; then bad="keep_min must be 1..1000"; fi
		if [ -n "$bad" ]; then
			[ "$raw" = "${SETTINGS_BAD:-}" ] ||
				warn "settings: the admin backup settings are invalid ($bad); keeping the previous values"
			SETTINGS_BAD=$raw
			return 0
		fi
	fi
	SETTINGS_BAD=
	EFF_TIME=${t:-$ENV_TIME}
	EFF_KEEP_DAYS=${kd:-$ENV_KEEP_DAYS}
	EFF_KEEP_MIN=${km:-$ENV_KEEP_MIN}
	EFF_PAUSED=${p:-false}
	EFF_SOURCE='env'
	[ -z "$t$kd$km$p" ] || EFF_SOURCE='admin'
}

# ─── Heartbeat ────────────────────────────────────────────────────────────────

# destination_desc: redacted destination, "s3:<bucket>/<prefix>" or
# "remote:<scheme>/<prefix>" (a local path is "local"). Never the endpoint
# or any key.
destination_desc() {
	prefix=${BACKUP_PREFIX:-spotify-tracker}
	prefix=${prefix#/}
	prefix=${prefix%/}
	if [ -n "${BACKUP_REMOTE:-}" ]; then
		case $BACKUP_REMOTE in
		*:*) scheme=${BACKUP_REMOTE%%:*} ;;
		*) scheme=local ;;
		esac
		case $scheme in *[!A-Za-z0-9_-]* | '') scheme=remote ;; esac
		printf 'remote:%s/%s\n' "$scheme" "$prefix"
	elif [ -n "${BACKUP_S3_BUCKET:-}" ]; then
		printf 's3:%s/%s\n' "$BACKUP_S3_BUCKET" "$prefix"
	fi
}

# recipients_list: the age public keys, one per line (at most 20).
recipients_list() {
	f=$(mktemp "${TMPDIR:-/tmp}/backupctl-rcpt.XXXXXX") || return 0
	(write_recipients "$f") >/dev/null 2>&1
	grep -E '^[A-Za-z0-9+/=:._-]{8,200}$' "$f" | head -n 20
	rm -f "$f"
}

# heartbeat: upserts the backup_agent row. Uses AGENT_CONFIGURED,
# AGENT_ERROR, EFF_* and NEXT_RUN (epoch, empty when none).
heartbeat() {
	[ "$STATUS_DB" = 1 ] || return 0
	next=$NEXT_RUN
	[ "$EFF_PAUSED" != true ] || next=
	status_sql "write the heartbeat" "INSERT INTO backup_agent (id, last_seen_at, version, configured, config_error,
			destination, recipients, backup_time, keep_days, keep_min, paused, next_run_at, settings_source)
		VALUES (1, now(), NULLIF(:'version', ''), :'configured'::boolean, left(NULLIF(:'cerr', ''), 500),
			NULLIF(:'dest', ''), coalesce(string_to_array(NULLIF(:'recips', ''), chr(10)), '{}'),
			NULLIF(:'btime', ''), NULLIF(:'kdays', '')::integer, NULLIF(:'kmin', '')::integer,
			:'paused'::boolean, to_timestamp(NULLIF(:'next', '')::bigint), :'source')
		ON CONFLICT (id) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at, version = EXCLUDED.version,
			configured = EXCLUDED.configured, config_error = EXCLUDED.config_error,
			destination = EXCLUDED.destination, recipients = EXCLUDED.recipients,
			backup_time = EXCLUDED.backup_time, keep_days = EXCLUDED.keep_days, keep_min = EXCLUDED.keep_min,
			paused = EXCLUDED.paused, next_run_at = EXCLUDED.next_run_at, settings_source = EXCLUDED.settings_source" \
		"version=${BACKUP_VERSION:-}" "configured=$AGENT_CONFIGURED" "cerr=$(short_error "$AGENT_ERROR")" \
		"dest=$AGENT_DEST" "recips=$AGENT_RECIPIENTS" "btime=$EFF_TIME" "kdays=$(uint_or_empty "$EFF_KEEP_DAYS")" \
		"kmin=$(uint_or_empty "$EFF_KEEP_MIN")" "paused=$EFF_PAUSED" "next=$next" "source=$EFF_SOURCE" >/dev/null
}

# ─── Requests ─────────────────────────────────────────────────────────────────

# request_claim: claims the oldest unclaimed request; prints "id|kind|by".
request_claim() {
	status_sql "claim a request" "UPDATE backup_requests SET claimed_at = now()
		WHERE id = (SELECT id FROM backup_requests WHERE claimed_at IS NULL
			ORDER BY requested_at, id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id, kind, coalesce(requested_by, '')"
}

# request_done ID RUN_ID ERROR: marks a claimed request done. On failure the
# result is kept in REQ_PENDING and retried on the next loop.
request_done() {
	if status_sql "mark the request done" "UPDATE backup_requests SET done_at = now(),
		run_id = NULLIF(:'run', '')::bigint, error = left(NULLIF(:'err', ''), 500) WHERE id = :'id'::bigint" \
		"id=$1" "run=$2" "err=$(short_error "$3")" >/dev/null; then
		REQ_PENDING=
	else
		REQ_PENDING="$1|$2|$3"
	fi
}

# uint_or_empty VALUE: VALUE when it is a small whole number, else empty.
uint_or_empty() {
	case $1 in
	'' | *[!0-9]*) ;;
	*) [ "${#1}" -gt 6 ] || printf '%s\n' "$1" ;;
	esac
}
