# shellcheck shell=sh
# backupctl backup / prune / list. Sourced by backupctl.

# cmd_backup: pg_dump | age → temp file → upload → verify size → manifest.
# Exits non-zero on any failure. Run it in a subshell from the scheduler.
cmd_backup() {
	has_recipient || die "refusing to back up: no age recipient (set BACKUP_AGE_RECIPIENT or BACKUP_AGE_RECIPIENTS_FILE); backups are never stored unencrypted"
	require_storage
	setup_db
	umask 077
	make_workdir
	trap 'cleanup_workdir' EXIT
	trap 'cleanup_workdir; exit 130' INT TERM

	recipients="$WORKDIR/recipients"
	write_recipients "$recipients" || die "refusing to back up: the age recipient list is empty"
	if ! printf x | age -R "$recipients" >/dev/null 2>"$WORKDIR/age.err"; then
		die "invalid age recipient: $(head -n 1 "$WORKDIR/age.err")"
	fi

	now=$(date -u +%s)
	stamp=$(date -u -d "@$now" +%Y%m%dT%H%M%SZ)
	created_at=$(date -u -d "@$now" +%Y-%m-%dT%H:%M:%SZ)
	name="$NAME_BASE-$stamp.dump.age"
	path=$(object_path "$name")

	info "backup $name: reading database info"
	db_name=$(sql 'SELECT current_database()' 2>"$WORKDIR/psql.err") ||
		die "cannot connect to the database: $(tail -n 1 "$WORKDIR/psql.err")"
	server_version=$(sql 'SHOW server_version') || die "cannot read the server version"
	goose_version=
	if [ "$(sql "SELECT to_regclass('public.goose_db_version') IS NOT NULL")" = t ]; then
		goose_version=$(sql 'SELECT max(version_id) FROM goose_db_version WHERE is_applied') || goose_version=
	fi
	dump_version=$(pg_dump --version)

	# Test hook (roundtrip.sh): a slow backup, to stop the service mid-run.
	[ -z "${BACKUP_DEBUG_SLEEP:-}" ] || sleep "$BACKUP_DEBUG_SLEEP"
	info "backup $name: dumping and encrypting"
	out="$WORKDIR/$name"
	# pg_dump → tee → age, with tee's second copy read by pg_restore --list
	# (then drained), so the archive's table list is checked without ever
	# writing plaintext to disk. Fifos instead of pipes: every exit status is
	# checked (POSIX sh has no pipefail).
	mkfifo "$WORKDIR/dump" "$WORKDIR/enc" "$WORKDIR/toc.pipe" || die "cannot create fifos"
	pg_dump -Fc -Z 6 --no-owner --no-privileges >"$WORKDIR/dump" 2>"$WORKDIR/pg_dump.err" &
	dump_pid=$!
	tee "$WORKDIR/toc.pipe" <"$WORKDIR/dump" >"$WORKDIR/enc" 2>"$WORKDIR/tee.err" &
	tee_pid=$!
	# Only three counters ("rc tables entries") reach the disk, not the TOC.
	toc_check "$WORKDIR/toc.pipe" >"$WORKDIR/toc.result" &
	toc_pid=$!
	age -R "$recipients" -o "$out" <"$WORKDIR/enc" 2>"$WORKDIR/age.err"
	age_rc=$?
	wait "$dump_pid"
	dump_rc=$?
	wait "$tee_pid"
	tee_rc=$?
	wait "$toc_pid"
	if [ "$dump_rc" -ne 0 ]; then
		die "pg_dump failed (exit $dump_rc): $(tail -n 3 "$WORKDIR/pg_dump.err" | tr '\n' ' ')"
	fi
	[ "$age_rc" -eq 0 ] || die "age failed (exit $age_rc): $(head -n 1 "$WORKDIR/age.err")"
	[ "$tee_rc" -eq 0 ] || die "tee failed (exit $tee_rc): $(head -n 1 "$WORKDIR/tee.err")"
	read -r toc_rc tables _ <"$WORKDIR/toc.result" || toc_rc=99
	[ "$toc_rc" = 0 ] ||
		die "the dump is not a readable archive: $(head -n 1 "$WORKDIR/toc.err")"
	[ "$tables" -gt 0 ] || die "the dump contains no tables (wrong or empty database?); not uploading it"

	size=$(wc -c <"$out" | tr -d ' ')
	sha=$(sha256sum "$out" | cut -d' ' -f1)
	[ "$size" -gt 0 ] || die "encrypted dump is empty"

	info "backup $name: uploading $(human_size "$size") to the $STORAGE_DESC"
	rcl copyto "$out" "$path" || die "upload failed"
	remote_size=$(rcl lsjson "$path" | tr ',' '\n' | sed -n 's/.*"Size":\([0-9]*\).*/\1/p' | head -n 1)
	if [ "$remote_size" != "$size" ]; then
		rcl deletefile "$path" >/dev/null 2>&1 || true
		die "uploaded size mismatch (local $size, remote ${remote_size:-missing}); upload removed"
	fi

	manifest="$out.json"
	{
		printf '{\n'
		printf '  "name": %s,\n' "$(json_str "$name")"
		printf '  "created_at": %s,\n' "$(json_str "$created_at")"
		printf '  "size": %s,\n' "$size"
		printf '  "sha256": %s,\n' "$(json_str "$sha")"
		printf '  "pg_dump_version": %s,\n' "$(json_str "$dump_version")"
		printf '  "server_version": %s,\n' "$(json_str "$server_version")"
		printf '  "database": %s,\n' "$(json_str "$db_name")"
		if [ -n "$goose_version" ]; then
			printf '  "goose_version": %s,\n' "$goose_version"
		else
			printf '  "goose_version": null,\n'
		fi
		printf '  "format": "pg_dump-custom+age"\n'
		printf '}\n'
	} >"$manifest"
	rcl copyto "$manifest" "$path.json" || {
		rcl deletefile "$path" >/dev/null 2>&1 || true
		die "manifest upload failed; backup removed"
	}

	# backup_with_status: the result, for the status table.
	[ -z "${RESULT_FILE:-}" ] || printf '%s %s %s %s\n' "$name" "$size" "$sha" "$tables" >"$RESULT_FILE"
	info "backup $name: done ($tables tables, $(human_size "$size"), sha256 $sha, schema version ${goose_version:-none})"
	printf '%s\n' "$name"
}

# backup_with_status [TRIGGER [REQUESTED_BY]]: one backup in a subshell,
# then status files, ping, and the backup_runs row (best-effort; see
# backupctl-status.sh). Sets RUN_ID (empty when not recorded) and
# RUN_ERROR; a success that could not be recorded is kept in UNRECORDED_RUN.
# The backup's stdout (its name) passes through.
backup_with_status() {
	bws_trigger=${1:-manual}
	bws_by=${2:-}
	bws_started=$(date -u +%s)
	RUN_ID=$(run_start "$bws_trigger" "$bws_by") || RUN_ID=
	RUN_ERROR=
	RESULT_FILE=$(mktemp "${TMPDIR:-/tmp}/backupctl-res.XXXXXX") || RESULT_FILE=
	RESULT_ERR_FILE=$(mktemp "${TMPDIR:-/tmp}/backupctl-err.XXXXXX") || RESULT_ERR_FILE=
	if [ "${BACKUP_IN_BACKGROUND:-}" = 1 ]; then
		# Scheduler: a background job plus wait, so SIGTERM runs the stop
		# trap at once instead of after the backup (see on_stop).
		(cmd_backup) &
		BACKUP_PID=$!
		wait "$BACKUP_PID"
		rc=$?
		BACKUP_PID=
	else
		(cmd_backup)
		rc=$?
	fi
	bws_name='' bws_size='' bws_sha='' bws_tables=''
	if [ -n "$RESULT_FILE" ] && [ -s "$RESULT_FILE" ]; then
		read -r bws_name bws_size bws_sha bws_tables <"$RESULT_FILE" || true
	fi
	[ -z "$RESULT_ERR_FILE" ] || RUN_ERROR=$(cat "$RESULT_ERR_FILE" 2>/dev/null)
	rm -f "$RESULT_FILE" "$RESULT_ERR_FILE"
	RESULT_FILE='' RESULT_ERR_FILE=''
	if [ "$rc" -eq 0 ]; then
		RUN_ERROR=
		status_write last_success "$(date -u +%s)"
		ping_url ""
		bws_status=success
	else
		[ -n "$RUN_ERROR" ] || RUN_ERROR="backup failed (exit $rc)"
		status_write last_error "$(date -u +%s)"
		err "backup failed"
		ping_url /fail
		bws_status=failed
	fi
	# shellcheck disable=SC2034 # UNRECORDED_RUN is read by backupctl-run.sh
	if bws_id=$(run_finish "$RUN_ID" "$bws_trigger" "$bws_by" "$bws_started" "$bws_status" \
		"$bws_name" "$bws_size" "$bws_sha" "$bws_tables" "$(short_error "$RUN_ERROR")"); then
		RUN_ID=$bws_id
		UNRECORDED_RUN=
	elif [ "$rc" -eq 0 ]; then
		# Recorded later, once the status tables exist (status_tick).
		UNRECORDED_RUN="$bws_trigger|$bws_started|$bws_name|$bws_size|$bws_sha|$bws_tables|$bws_by"
	fi
	return "$rc"
}

is_uint() {
	case $1 in
	'' | *[!0-9]*) return 1 ;;
	esac
	return 0
}

# cmd_prune: deletes backups older than BACKUP_KEEP_DAYS, but always keeps
# the newest BACKUP_KEEP_MIN complete backups.
cmd_prune() {
	require_storage
	keep_days=${BACKUP_KEEP_DAYS:-30}
	keep_min=${BACKUP_KEEP_MIN:-7}
	is_uint "$keep_days" || die "BACKUP_KEEP_DAYS must be a whole number of days"
	is_uint "$keep_min" || die "BACKUP_KEEP_MIN must be a whole number"
	# The newest backup is never pruned.
	[ "$keep_min" -ge 1 ] || keep_min=1
	if [ "$keep_days" -eq 0 ]; then
		info "prune: BACKUP_KEEP_DAYS=0, keeping every backup"
		return 0
	fi
	cutoff_epoch=$(($(date -u +%s) - keep_days * 86400))
	cutoff="$NAME_BASE-$(date -u -d "@$cutoff_epoch" +%Y%m%dT%H%M%SZ).dump.age"

	# One listing for both sets; any listing error stops the prune, so a
	# failed list can never make every old backup look unprotected.
	listing=$(remote_listing) || die "prune: cannot list backups; nothing deleted"
	complete=$(complete_from "$listing" | cut -d';' -f1)
	all=$(names_from "$listing")
	victims=$(
		{
			printf '%s\n' "$complete" | sed -n "1,${keep_min}p" | sed 's/^/K /'
			printf '%s\n' "$all" | sed 's/^/A /'
		} | awk -v cutoff="$cutoff" '
			$1 == "K" { keep[$2] = 1; next }
			$1 == "A" && $2 != "" && !($2 in keep) && $2 < cutoff { print $2 }
		'
	)
	if [ -z "$victims" ]; then
		info "prune: nothing older than $keep_days days beyond the newest $keep_min"
		return 0
	fi
	count=0
	failed=0
	for v in $victims; do
		valid_name "$v" || continue
		dir=$(dirname "$(object_path "$v")")
		if rcl delete "$dir" --max-depth 1 --include "/$v" --include "/$v.json"; then
			info "prune: deleted $v"
			count=$((count + 1))
		else
			err "prune: could not delete $v"
			failed=1
		fi
	done
	rcl rmdirs --leave-root "$REMOTE_BASE" >/dev/null 2>&1 || true
	info "prune: deleted $count backup(s)"
	return "$failed"
}

cmd_list() {
	require_storage
	objs=$(list_objects) || die "cannot list backups"
	if [ -z "$objs" ]; then
		info "no backups found"
		return 0
	fi
	printf '%-44s %10s  %s\n' NAME SIZE 'UPLOADED (UTC)'
	printf '%s\n' "$objs" | while IFS=';' read -r n s t; do
		printf '%-44s %10s  %s\n' "$n" "$(human_size "$s")" "$t"
	done
}
