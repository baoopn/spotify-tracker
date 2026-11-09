# shellcheck shell=sh
# Globals are shared with the other sourced files (backupctl sources all);
# prune_effective changes the retention only in its subshell, on purpose.
# shellcheck disable=SC2034,SC2153,SC2030,SC2031
# backupctl run: the scheduler. Sourced by backupctl.
#
# The loop wakes every BACKUP_LOOP_SECONDS (default 60): it writes the
# heartbeat, reads the admin settings (settings key 'backup'), runs queued
# admin requests, and starts the daily backup at the effective time (UTC).
# The database status is best-effort (backupctl-status.sh): with the
# database down, backups still run on the env settings.

SLEEP_PID=
HB_PID=
RETRY_AT=
NEXT_RUN=
REQ_PENDING=
CUR_REQ=
CUR_REQ_RUN=
BACKUP_PID=
UNRECORDED_RUN=
STATUS_PREV=

# sleep_for SECONDS: interruptible by SIGTERM (docker stop).
sleep_for() {
	[ "$1" -gt 0 ] || return 0
	sleep "$1" &
	SLEEP_PID=$!
	wait "$SLEEP_PID"
	SLEEP_PID=
}

# on_stop: SIGTERM (docker stop, 10 s). A running backup is abandoned, not
# awaited (the container's processes end with it); its run row and the
# current request are marked interrupted, best-effort.
on_stop() {
	[ -z "$SLEEP_PID" ] || kill "$SLEEP_PID" 2>/dev/null
	hb_stop
	if [ -n "$BACKUP_PID" ]; then
		kill "$BACKUP_PID" 2>/dev/null
		info "stopping: abandoning the running backup"
		status_stopped "$RUN_ID" "$CUR_REQ" "${CUR_REQ_RUN:+$RUN_ID}"
	elif [ -n "$CUR_REQ" ]; then
		status_stopped "" "$CUR_REQ" ""
	fi
	info "stopping"
	exit 0
}

# hb_start / hb_stop: heartbeat in the background while a backup runs, so
# a long backup does not look like an offline agent.
hb_start() {
	[ "$STATUS_DB" = 1 ] || return 0
	(
		STATUS_ERR=$STATUS_DIR/db-hb.err
		while :; do
			sleep "$LOOP_SECONDS"
			heartbeat
		done
	) >/dev/null 2>&1 &
	HB_PID=$!
}

hb_stop() {
	[ -n "$HB_PID" ] || return 0
	kill "$HB_PID" 2>/dev/null
	wait "$HB_PID" 2>/dev/null
	HB_PID=
}

# next_run_epoch HH:MM: the next HH:MM (UTC) strictly after now.
next_run_epoch() {
	now=$(date -u +%s)
	t=$(date -u -d "$(date -u +%Y-%m-%d) $1:00" +%s) || die "cannot compute the next run time"
	[ "$t" -gt "$now" ] || t=$((t + 86400))
	printf '%s\n' "$t"
}

log_next() {
	if [ "$EFF_PAUSED" = true ]; then
		info "paused by the admin settings: no scheduled backups"
	else
		info "next backup at $(date -u -d "@$NEXT_RUN" +%Y-%m-%dT%H:%MZ)"
	fi
}

retry_minutes() {
	r=${BACKUP_RETRY_MINUTES:-30}
	is_uint "$r" || die "BACKUP_RETRY_MINUTES must be a whole number of minutes"
	printf '%s\n' "$r"
}

# prune_effective: prune with the effective (admin or env) retention.
prune_effective() {
	(
		BACKUP_KEEP_DAYS=$EFF_KEEP_DAYS
		BACKUP_KEEP_MIN=$EFF_KEEP_MIN
		cmd_prune
	)
}

# after_success: prune, refresh the inventory, apply status retention.
after_success() {
	prune_effective || warn "prune failed; will try again after the next backup"
	inventory_refresh || true
	status_retention
}

# scheduled_backup TRIGGER: one backup; a failure schedules one retry
# (unless this is the retry), a success prunes.
scheduled_backup() {
	hb_start
	if backup_with_status "$1" >/dev/null; then
		RETRY_AT=
		after_success
	elif [ "$1" != retry ]; then
		retry=$(retry_minutes)
		if [ "$retry" -gt 0 ]; then
			info "retrying in $retry minute(s)"
			RETRY_AT=$(($(date -u +%s) + retry * 60))
		fi
	fi
	hb_stop
}

# status_tick: once per loop. Database check, pending request result,
# admin settings, heartbeat.
status_tick() {
	status_check
	was=$STATUS_PREV
	STATUS_PREV=$STATUS_DB
	[ "$STATUS_DB" = 1 ] || return 0
	# The status tables appeared (migrations ran after this service started)
	# or the database is back: record what happened meanwhile.
	[ "$was" != 0 ] || status_appeared
	if [ -n "$REQ_PENDING" ]; then
		IFS='|' read -r p_id p_run p_err <<EOF
$REQ_PENDING
EOF
		request_done "$p_id" "$p_run" "$p_err"
	fi
	old="$EFF_TIME|$EFF_KEEP_DAYS|$EFF_KEEP_MIN|$EFF_PAUSED"
	old_time=$EFF_TIME
	old_paused=$EFF_PAUSED
	settings_apply
	if [ "$old" != "$EFF_TIME|$EFF_KEEP_DAYS|$EFF_KEEP_MIN|$EFF_PAUSED" ]; then
		info "settings ($EFF_SOURCE): daily at $EFF_TIME UTC, keep $EFF_KEEP_DAYS days (at least $EFF_KEEP_MIN), paused $EFF_PAUSED"
		if [ "$AGENT_CONFIGURED" = true ] &&
			{ [ "$old_time" != "$EFF_TIME" ] || [ "$old_paused" != "$EFF_PAUSED" ]; }; then
			NEXT_RUN=$(next_run_epoch "$EFF_TIME")
			[ "$EFF_PAUSED" != true ] || RETRY_AT=
			log_next
		fi
	fi
	if [ "$EFF_PAUSED" = true ]; then status_write paused 1; else rm -f "$STATUS_DIR/paused"; fi
	heartbeat
}

# status_appeared: records a success that had no run row, and refreshes the
# inventory.
status_appeared() {
	if [ -n "$UNRECORDED_RUN" ]; then
		IFS='|' read -r u_trigger u_started u_name u_size u_sha u_tables u_by <<EOF
$UNRECORDED_RUN
EOF
		if run_finish "" "$u_trigger" "$u_by" "$u_started" success "$u_name" "$u_size" "$u_sha" "$u_tables" "" >/dev/null; then
			UNRECORDED_RUN=
		fi
	fi
	[ "$AGENT_CONFIGURED" != true ] || inventory_refresh || true
}

# process_requests: runs queued admin requests, oldest first.
process_requests() {
	[ "$STATUS_DB" = 1 ] || return 0
	while [ -z "$REQ_PENDING" ] && req=$(request_claim) && [ -n "$req" ]; do
		IFS='|' read -r rid rkind rby <<EOF
$req
EOF
		info "request $rid: $rkind (from the admin dashboard)"
		CUR_REQ=$rid
		CUR_REQ_RUN=
		if [ "$AGENT_CONFIGURED" != true ]; then
			request_done "$rid" "" "backups are not configured: ${AGENT_ERROR:-no destination or recipient set}"
			CUR_REQ=
			continue
		fi
		case $rkind in
		backup)
			hb_start
			CUR_REQ_RUN=1
			if backup_with_status manual "$rby" >/dev/null; then
				after_success
				request_done "$rid" "$RUN_ID" ""
			else
				request_done "$rid" "$RUN_ID" "$RUN_ERROR"
			fi
			hb_stop
			;;
		refresh)
			if inventory_refresh; then
				request_done "$rid" "" ""
			else
				request_done "$rid" "" "cannot list backups"
			fi
			;;
		*) request_done "$rid" "" "unknown request kind" ;;
		esac
		CUR_REQ=
		CUR_REQ_RUN=
		status_check || break
	done
}

# unconfigured_loop LEVEL MESSAGE: stays up without backups. Logs MESSAGE
# once a day (an ERROR also pings /fail), heartbeats and answers requests.
unconfigured_loop() {
	AGENT_CONFIGURED=false
	last=0
	while :; do
		now=$(date -u +%s)
		if [ $((now - last)) -ge 86400 ]; then
			last=$now
			if [ "$1" = ERROR ]; then
				err "$2"
				ping_url /fail
			else
				warn "$2"
			fi
		fi
		status_tick
		process_requests
		sleep_for "$LOOP_SECONDS"
	done
}

# idle REASON: unconfigured service; stay up (healthy) instead of crash-looping.
idle() {
	status_write idle "$1"
	AGENT_ERROR=
	unconfigured_loop WARN "offsite backups are disabled: $1 (see docs/backup.md); idling"
}

# misconfigured REASON: some backup settings are set but not usable. Stay up
# (no crash loop), but log an ERROR, ping /fail and report unhealthy.
misconfigured() {
	status_write misconfigured "$1"
	AGENT_ERROR=$1
	unconfigured_loop ERROR "offsite backups are misconfigured: $1 (see docs/backup.md); no backups will run"
}

cmd_run() {
	trap on_stop TERM INT
	mkdir -p "$STATUS_DIR" 2>/dev/null
	rm -f "$STATUS_DIR/idle" "$STATUS_DIR/misconfigured" "$STATUS_DIR/first_due" "$STATUS_DIR/paused"
	is_uint "$LOOP_SECONDS" && [ "$LOOP_SECONDS" -gt 0 ] || LOOP_SECONDS=60

	ENV_TIME=${BACKUP_TIME:-03:00}
	ENV_KEEP_DAYS=${BACKUP_KEEP_DAYS:-30}
	ENV_KEEP_MIN=${BACKUP_KEEP_MIN:-7}
	EFF_TIME=$ENV_TIME EFF_KEEP_DAYS=$ENV_KEEP_DAYS EFF_KEEP_MIN=$ENV_KEEP_MIN
	EFF_PAUSED=false EFF_SOURCE=env
	AGENT_CONFIGURED=false AGENT_ERROR=
	AGENT_DEST=$(destination_desc)
	AGENT_RECIPIENTS=$(recipients_list)
	BACKUP_IN_BACKGROUND=1
	if db_usable && status_check; then
		status_startup
	fi
	STATUS_PREV=$STATUS_DB

	# Nothing set at all: an install that does not use off-site backups.
	config_untouched && idle "no BACKUP_AGE_RECIPIENT and no destination are set"
	problems=$(config_problems)
	[ -z "$problems" ] || misconfigured "$problems"
	setup_storage || misconfigured "no usable destination"
	case $ENV_TIME in
	[01][0-9]:[0-5][0-9] | 2[0-3]:[0-5][0-9]) ;;
	*) misconfigured "BACKUP_TIME must be HH:MM (24-hour, UTC), e.g. 03:00" ;;
	esac
	(retry_minutes >/dev/null) 2>/dev/null || misconfigured "BACKUP_RETRY_MINUTES must be a whole number of minutes"
	db_usable || misconfigured "no usable database settings (DATABASE_URL or PG*)"

	AGENT_CONFIGURED=true
	status_tick
	NEXT_RUN=$(next_run_epoch "$EFF_TIME")
	info "scheduler: daily at $EFF_TIME UTC to the $STORAGE_DESC, keep $EFF_KEEP_DAYS days (at least $EFF_KEEP_MIN)"
	case ${BACKUP_ON_START:-false} in
	true | 1 | yes)
		status_write first_due "$(date -u +%s)"
		if [ "$EFF_PAUSED" = true ]; then
			info "BACKUP_ON_START: skipped, paused by the admin settings"
		else
			info "BACKUP_ON_START: backing up now"
			scheduled_backup start
		fi
		;;
	*) status_write first_due "$NEXT_RUN" ;;
	esac
	log_next

	while :; do
		status_tick
		process_requests
		now=$(date -u +%s)
		if [ "$EFF_PAUSED" != true ]; then
			if [ -n "$RETRY_AT" ] && [ "$now" -ge "$RETRY_AT" ]; then
				RETRY_AT=
				scheduled_backup retry
			elif [ "$now" -ge "$NEXT_RUN" ]; then
				NEXT_RUN=$(next_run_epoch "$EFF_TIME")
				scheduled_backup schedule
				log_next
			fi
		fi
		now=$(date -u +%s)
		wait_s=$LOOP_SECONDS
		if [ "$EFF_PAUSED" != true ]; then
			for t in "$NEXT_RUN" "$RETRY_AT"; do
				[ -n "$t" ] || continue
				[ $((t - now)) -ge "$wait_s" ] || wait_s=$((t - now))
			done
		fi
		[ "$wait_s" -ge 1 ] || wait_s=1
		sleep_for "$wait_s"
	done
}
