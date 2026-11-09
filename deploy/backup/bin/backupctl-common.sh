# shellcheck shell=sh
# Shared helpers for backupctl: logging, configuration, storage and database
# access. Sourced by backupctl; not executable on its own.
#
# Never print secrets: log lines name settings, never their values.

STATUS_DIR=${BACKUP_STATUS_DIR:-/tmp/backup-status}
NAME_BASE=spotify-tracker
NAME_GLOB="$NAME_BASE-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z.dump.age"

# ─── Logging ──────────────────────────────────────────────────────────────────

log() {
	level=$1
	shift
	printf '%s %-5s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$level" "$*" >&2
}
info() { log INFO "$@"; }
warn() { log WARN "$@"; }
err() { log ERROR "$@"; }
die() {
	err "$@"
	exit 1
}

# ─── Temp files ───────────────────────────────────────────────────────────────

# make_workdir: creates a private temp directory in $WORKDIR. The caller's
# EXIT trap must call cleanup_workdir.
make_workdir() {
	WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/backupctl.XXXXXX") || die "cannot create a temp directory"
	chmod 700 "$WORKDIR"
}

cleanup_workdir() {
	if [ -n "${WORKDIR:-}" ] && [ -d "$WORKDIR" ]; then
		# Test hook (roundtrip.sh): list the regular files left in the work
		# dir, to prove no plaintext dump was staged there.
		if [ "${BACKUP_DEBUG_WORKDIR:-}" = 1 ]; then
			find "$WORKDIR" -type f | while IFS= read -r f; do
				log DEBUG "workdir file: ${f##*/} $(wc -c <"$f" | tr -d ' ')"
			done
		fi
		rm -rf "$WORKDIR"
	fi
	WORKDIR=
}

# toc_check INPUT: reads a pg_dump custom archive from INPUT (a fifo or a
# file) with pg_restore --list, then drains INPUT. Prints "rc tables entries".
# The listing is counted in a pipe, never stored; errors go to toc.err.
toc_check() {
	{
		pg_restore --list 2>"$WORKDIR/toc.err"
		echo "#rc $?"
		cat >/dev/null
	} <"$1" | awk '
		/^#rc / { rc = $2; next }
		/^[0-9]+; [0-9]+ [0-9]+ TABLE / { t++ }
		/^[0-9]+;/ { e++ }
		END { print (rc == "" ? 99 : rc), t + 0, e + 0 }'
}

# ─── Status files (health) ────────────────────────────────────────────────────

status_write() {
	mkdir -p "$STATUS_DIR" 2>/dev/null || return 0
	printf '%s\n' "$2" >"$STATUS_DIR/$1" 2>/dev/null || true
}

status_read() {
	if [ -r "$STATUS_DIR/$1" ]; then
		cat "$STATUS_DIR/$1"
	fi
}

# ping SUFFIX: optional healthchecks.io-style ping ("" on success, "/fail").
# Short timeout, errors ignored, URL never logged.
ping_url() {
	[ -n "${BACKUP_PING_URL:-}" ] || return 0
	wget -q -T 10 -O /dev/null "${BACKUP_PING_URL%/}$1" >/dev/null 2>&1 || warn "ping failed (ignored)"
}

# ─── Encryption ───────────────────────────────────────────────────────────────

# POSIX sh has no arrays, so recipients go to a file (age -R FILE), one per
# line, instead of a list of -r flags.
#
# write_recipients FILE: collects BACKUP_AGE_RECIPIENT (space or comma
# separated) and BACKUP_AGE_RECIPIENTS_FILE into FILE. Returns 1 if empty.
write_recipients() {
	: >"$1"
	if [ -n "${BACKUP_AGE_RECIPIENT:-}" ]; then
		set -f
		for r in $(printf '%s' "$BACKUP_AGE_RECIPIENT" | tr ',' ' '); do
			printf '%s\n' "$r" >>"$1"
		done
		set +f
	fi
	if [ -n "${BACKUP_AGE_RECIPIENTS_FILE:-}" ]; then
		[ -r "$BACKUP_AGE_RECIPIENTS_FILE" ] || die "BACKUP_AGE_RECIPIENTS_FILE is not readable"
		sed -e 's/#.*//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e '/^$/d' \
			"$BACKUP_AGE_RECIPIENTS_FILE" >>"$1"
	fi
	[ -s "$1" ]
}

has_recipient() {
	[ -n "${BACKUP_AGE_RECIPIENT:-}" ] || [ -n "${BACKUP_AGE_RECIPIENTS_FILE:-}" ]
}

# ─── Storage (rclone, configured from env only) ───────────────────────────────

# setup_storage: sets REMOTE_BASE (rclone path of the backup prefix) and
# exports the RCLONE_CONFIG_BACKUP_* settings for the S3 remote. Returns 1
# when no destination is configured.
setup_storage() {
	export RCLONE_CONFIG="${RCLONE_CONFIG:-/dev/null}"
	prefix=${BACKUP_PREFIX:-spotify-tracker}
	prefix=${prefix#/}
	prefix=${prefix%/}
	if [ -n "${BACKUP_REMOTE:-}" ]; then
		REMOTE_BASE="${BACKUP_REMOTE%/}/$prefix"
		# shellcheck disable=SC2034
		STORAGE_DESC="rclone remote"
		return 0
	fi
	if [ -z "${BACKUP_S3_BUCKET:-}" ] || [ -z "${BACKUP_S3_ENDPOINT:-}" ] ||
		[ -z "${BACKUP_S3_ACCESS_KEY_ID:-}" ] || [ -z "${BACKUP_S3_SECRET_ACCESS_KEY:-}" ]; then
		return 1
	fi
	export RCLONE_CONFIG_BACKUP_TYPE=s3
	export RCLONE_CONFIG_BACKUP_PROVIDER="${BACKUP_S3_PROVIDER:-Cloudflare}"
	export RCLONE_CONFIG_BACKUP_ENDPOINT="$BACKUP_S3_ENDPOINT"
	export RCLONE_CONFIG_BACKUP_REGION="${BACKUP_S3_REGION:-auto}"
	export RCLONE_CONFIG_BACKUP_ACCESS_KEY_ID="$BACKUP_S3_ACCESS_KEY_ID"
	export RCLONE_CONFIG_BACKUP_SECRET_ACCESS_KEY="$BACKUP_S3_SECRET_ACCESS_KEY"
	export RCLONE_CONFIG_BACKUP_NO_CHECK_BUCKET=true
	export RCLONE_CONFIG_BACKUP_ENV_AUTH=false
	REMOTE_BASE="backup:${BACKUP_S3_BUCKET}/$prefix"
	# shellcheck disable=SC2034 # used by the sourcing scripts
	STORAGE_DESC="S3 bucket"
}

require_storage() {
	setup_storage || die "no backup destination: set BACKUP_S3_ENDPOINT, BACKUP_S3_BUCKET, BACKUP_S3_ACCESS_KEY_ID and BACKUP_S3_SECRET_ACCESS_KEY, or BACKUP_REMOTE"
}


# config_problems: prints what is missing when backups are partly configured
# (empty output = complete). config_untouched: nothing BACKUP_* is set at all.
s3_missing() {
	for v in BACKUP_S3_ENDPOINT BACKUP_S3_BUCKET BACKUP_S3_ACCESS_KEY_ID BACKUP_S3_SECRET_ACCESS_KEY; do
		eval "val=\${$v:-}"
		# shellcheck disable=SC2154 # set by eval
		[ -n "$val" ] || printf '%s ' "$v"
	done
}

config_untouched() {
	! has_recipient && [ -z "${BACKUP_REMOTE:-}" ] && [ "$(s3_missing | wc -w)" -eq 4 ]
}

config_problems() {
	has_recipient || printf '%s' "BACKUP_AGE_RECIPIENT is not set. "
	if [ -z "${BACKUP_REMOTE:-}" ]; then
		missing=$(s3_missing)
		[ -z "$missing" ] || printf 'incomplete destination, missing: %s' "$missing"
	fi
}

# rcl ARGS...: rclone with quiet output; errors go to stderr.
rcl() {
	rclone -q --retries 3 --low-level-retries 10 "$@"
}

# object_path NAME: remote path of a backup object (<prefix>/<YYYY>/<MM>/NAME).
object_path() {
	stamp=${1#"$NAME_BASE"-}
	printf '%s/%s/%s/%s\n' "$REMOTE_BASE" "$(printf %s "$stamp" | cut -c1-4)" \
		"$(printf %s "$stamp" | cut -c5-6)" "$1"
}

# remote_listing: prints "path;size;modtime" for every backup object (dumps
# and manifests). rclone's stderr is kept apart from the listing; any error
# fails, except a missing prefix (no backups yet), which is an empty list.
remote_listing() {
	errf=$(mktemp "${TMPDIR:-/tmp}/backupctl-ls.XXXXXX") || return 1
	if out=$(rcl lsf -R --files-only --format pst --separator ';' \
		--include '*.dump.age' --include '*.dump.age.json' "$REMOTE_BASE" 2>"$errf"); then
		rm -f "$errf"
		[ -z "$out" ] || printf '%s\n' "$out"
		return 0
	fi
	if [ -z "$out" ] && grep -q 'directory not found' "$errf" &&
		! grep -v 'directory not found' "$errf" | grep -q ERROR; then
		rm -f "$errf"
		return 0
	fi
	sed 's/^/rclone: /' "$errf" | tail -n 5 >&2
	rm -f "$errf"
	return 1
}

# complete_from LISTING: "name;size;modtime" for every backup that has both
# its .dump.age and its .json manifest, newest first. Lone files (an
# interrupted upload) are ignored; prune still removes them once old.
complete_from() {
	printf '%s\n' "$1" | awk -F';' '
		NF < 2 { next }
		{ n = $1; sub(/.*\//, "", n) }
		n ~ /\.dump\.age\.json$/ { sub(/\.json$/, "", n); man[n] = 1; next }
		n ~ /\.dump\.age$/ { size[n] = $2; t[n] = $3 }
		END { for (n in size) if (n in man) print n ";" size[n] ";" t[n] }
	' | sort -r
}

# names_from LISTING: every backup name present (dump or manifest), newest first.
names_from() {
	printf '%s\n' "$1" | cut -d';' -f1 | sed -e 's#.*/##' -e 's/\.json$//' -e '/^$/d' | sort -ru
}

list_objects() {
	listing=$(remote_listing) || return 1
	complete_from "$listing"
}

# valid_name NAME: accepts only names this tool creates.
valid_name() {
	# shellcheck disable=SC2254
	case $1 in
	$NAME_GLOB) return 0 ;;
	esac
	return 1
}

# resolve_name latest|NAME: prints the backup name or fails.
resolve_name() {
	want=${1:-latest}
	if [ "$want" = latest ]; then
		objs=$(list_objects) || die "cannot list backups"
		name=$(printf '%s\n' "$objs" | head -n 1 | cut -d';' -f1)
		[ -n "$name" ] || die "no backups found"
		printf '%s\n' "$name"
		return 0
	fi
	case $want in
	*.dump.age) ;;
	*) want="$want.dump.age" ;;
	esac
	valid_name "$want" || die "not a backup name: expected $NAME_BASE-YYYYMMDDTHHMMSSZ.dump.age"
	printf '%s\n' "$want"
}

# ─── Database ─────────────────────────────────────────────────────────────────

urldecode() {
	printf '%s' "$1" | awk '
		function hexval(h,   i, n, c) {
			n = 0; h = tolower(h)
			for (i = 1; i <= 2; i++) {
				c = index("0123456789abcdef", substr(h, i, 1)) - 1
				if (c < 0) return -1
				n = n * 16 + c
			}
			return n
		}
		{
			s = $0; out = ""
			for (i = 1; i <= length(s); i++) {
				c = substr(s, i, 1)
				if (c == "%" && (v = hexval(substr(s, i + 1, 2))) >= 0) { out = out sprintf("%c", v); i += 2 }
				else out = out c
			}
			printf "%s", out
		}'
}

# parse_database_url: exports libpq PG* variables from DATABASE_URL, so the
# password never appears in a command line (ps). Format:
# postgres[ql]://user:password@host:port/dbname?sslmode=...
parse_database_url() {
	url=$DATABASE_URL
	case $url in
	postgres://* | postgresql://*) rest=${url#*://} ;;
	*) die "DATABASE_URL must start with postgres:// or postgresql://" ;;
	esac
	unset PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE
	userinfo=
	case $rest in
	*@*)
		userinfo=${rest%@*}
		rest=${rest##*@}
		;;
	esac
	query=
	case $rest in
	*\?*)
		query=${rest#*\?}
		rest=${rest%%\?*}
		;;
	esac
	db=
	case $rest in
	*/*)
		db=${rest#*/}
		rest=${rest%%/*}
		;;
	esac
	case $rest in
	*,*) die "DATABASE_URL with several hosts is not supported; use PGHOST" ;;
	\[*\]:*)
		host=${rest%%]*}
		host=${host#\[}
		port=${rest##*]:}
		;;
	\[*\]) host=${rest#\[} host=${host%]} port= ;;
	*:*) host=${rest%:*} port=${rest##*:} ;;
	*) host=$rest port= ;;
	esac
	if [ -n "$userinfo" ]; then
		case $userinfo in
		*:*)
			PGUSER=$(urldecode "${userinfo%%:*}")
			PGPASSWORD=$(urldecode "${userinfo#*:}")
			export PGUSER PGPASSWORD
			;;
		*)
			PGUSER=$(urldecode "$userinfo")
			export PGUSER
			;;
		esac
	fi
	if [ -n "$host" ]; then
		PGHOST=$(urldecode "$host")
		export PGHOST
	fi
	[ -z "$port" ] || export PGPORT="$port"
	if [ -n "$db" ]; then
		PGDATABASE=$(urldecode "$db")
		export PGDATABASE
	fi
	old_ifs=$IFS
	IFS='&'
	set -f
	for kv in $query; do
		[ -n "$kv" ] || continue
		k=${kv%%=*}
		v=$(urldecode "${kv#*=}")
		case $k in
		sslmode) export PGSSLMODE="$v" ;;
		sslrootcert) export PGSSLROOTCERT="$v" ;;
		sslcert) export PGSSLCERT="$v" ;;
		sslkey) export PGSSLKEY="$v" ;;
		connect_timeout) export PGCONNECT_TIMEOUT="$v" ;;
		application_name) export PGAPPNAME="$v" ;;
		options) export PGOPTIONS="$v" ;;
		target_session_attrs) export PGTARGETSESSIONATTRS="$v" ;;
		*)
			IFS=$old_ifs
			die "unsupported DATABASE_URL parameter: $k (use the PG* variables instead)"
			;;
		esac
	done
	set +f
	IFS=$old_ifs
}

# setup_db: DATABASE_URL (parsed into PG*) wins over the PG* variables.
# DB_NAME is the database name for pg_restore -d (never a connection string).
setup_db() {
	if [ -n "${DATABASE_URL:-}" ]; then
		parse_database_url
	fi
	DB_NAME=${PGDATABASE:-${PGUSER:-}}
	[ -n "$DB_NAME" ] && [ -n "${PGHOST:-}${DATABASE_URL:-}" ] ||
		die "no database: set DATABASE_URL, or PGHOST, PGPORT, PGUSER, PGPASSWORD and PGDATABASE"
	case $DB_NAME in
	*=* | *://*) die "PGDATABASE must be a plain database name" ;;
	esac
	export PGDATABASE="$DB_NAME"
}

# sql QUERY: runs one query, prints unaligned tuples.
sql() {
	psql -X -q -A -t -v ON_ERROR_STOP=1 -c "$1"
}

json_str() {
	printf '"%s"' "$(printf %s "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\n\r')"
}

# manifest_field FILE KEY: reads a top-level string or number value from a
# manifest written by cmd_backup (one key per line).
manifest_field() {
	sed -n "s/^[[:space:]]*\"$2\":[[:space:]]*\"\{0,1\}\([^\",]*\)\"\{0,1\},\{0,1\}[[:space:]]*\$/\1/p" "$1" | head -n 1
}

human_size() {
	awk -v b="$1" 'BEGIN {
		split("B KiB MiB GiB TiB", u, " "); i = 1
		while (b >= 1024 && i < 5) { b /= 1024; i++ }
		printf (i == 1 ? "%d %s" : "%.1f %s"), b, u[i]
	}'
}
