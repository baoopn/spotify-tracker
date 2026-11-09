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
		rm -rf "$WORKDIR"
	fi
	WORKDIR=
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

# list_objects: prints "name;size;modtime" for every backup that has both its
# .dump.age and its .json manifest, newest first. Lone files (an interrupted
# upload) are ignored here; prune still removes them once old.
list_objects() {
	listing=$(rcl lsf -R --files-only --format pst --separator ';' \
		--include '*.dump.age' --include '*.dump.age.json' "$REMOTE_BASE" 2>&1) || {
		# A missing prefix (no backups yet) is not an error.
		case $listing in
		*"directory not found"* | *"not found"*) return 0 ;;
		esac
		printf '%s\n' "$listing" >&2
		return 1
	}
	printf '%s\n' "$listing" | awk -F';' '
		{ n = $1; sub(/.*\//, "", n) }
		n ~ /\.dump\.age\.json$/ { sub(/\.json$/, "", n); man[n] = 1; next }
		n ~ /\.dump\.age$/ { size[n] = $2; t[n] = $3 }
		END { for (n in size) if (n in man) print n ";" size[n] ";" t[n] }
	' | sort -r
}

# list_all_names: every backup name present in storage (dump or manifest),
# newest first. Used by prune.
list_all_names() {
	listing=$(rcl lsf -R --files-only --include '*.dump.age' --include '*.dump.age.json' \
		"$REMOTE_BASE" 2>&1) || {
		case $listing in
		*"directory not found"* | *"not found"*) return 0 ;;
		esac
		printf '%s\n' "$listing" >&2
		return 1
	}
	printf '%s\n' "$listing" | sed -e 's#.*/##' -e 's/\.json$//' | sort -ru
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

# DB_TARGET: the -d argument for psql/pg_dump/pg_restore. DATABASE_URL wins
# over the libpq PG* variables.
setup_db() {
	if [ -n "${DATABASE_URL:-}" ]; then
		DB_TARGET=$DATABASE_URL
	elif [ -n "${PGDATABASE:-}" ]; then
		DB_TARGET=$PGDATABASE
	else
		die "no database: set DATABASE_URL, or PGHOST, PGPORT, PGUSER, PGPASSWORD and PGDATABASE"
	fi
}

# sql QUERY: runs one query, prints unaligned tuples.
sql() {
	psql -X -q -A -t -v ON_ERROR_STOP=1 -d "$DB_TARGET" -c "$1"
}

json_str() {
	printf '"%s"' "$(printf %s "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\n\r')"
}

# manifest_field FILE KEY: reads a top-level string or number value from a
# manifest written by write_manifest (one key per line).
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
