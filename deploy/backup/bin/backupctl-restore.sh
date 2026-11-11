# shellcheck shell=sh
# backupctl restore / verify. Sourced by backupctl.

# parse_restore_args ARGS...: sets WANT, FORCE, TO_FILE, IDENTITY.
parse_restore_args() {
	WANT=
	FORCE=false
	TO_FILE=
	IDENTITY=${BACKUP_AGE_IDENTITY_FILE:-/run/secrets/backup-age-key}
	while [ $# -gt 0 ]; do
		case $1 in
		--force) FORCE=true ;;
		--to-file)
			[ $# -ge 2 ] || die "--to-file needs a path"
			TO_FILE=$2
			shift
			;;
		--to-file=*) TO_FILE=${1#--to-file=} ;;
		-i | --identity)
			[ $# -ge 2 ] || die "$1 needs a path (or - for stdin)"
			IDENTITY=$2
			shift
			;;
		-*) die "unknown option: $1" ;;
		*)
			[ -z "$WANT" ] || die "only one backup name may be given"
			WANT=$1
			;;
		esac
		shift
	done
	WANT=${WANT:-latest}
}


# Plaintext never touches the disk here: only the ciphertext is downloaded,
# and age decrypts into a fifo read by pg_restore. The one exception is
# --to-file, which writes the decrypted dump because it was asked for.

# load_identity: checks the identity source. A file is passed to age as is.
# With "-" the key is read from stdin into memory once, and fed to each age
# run through a fifo (never written to a file).
load_identity() {
	IDENTITY_KEY=
	ID_PID=
	if [ "$IDENTITY" = - ]; then
		[ ! -t 0 ] || die "-i - reads the age key from stdin: pipe it in (... < key.txt)"
		IDENTITY_KEY=$(cat) || die "cannot read the age key from stdin"
		[ -n "$IDENTITY_KEY" ] || die "no age key on stdin"
	elif [ ! -r "$IDENTITY" ] || [ -d "$IDENTITY" ]; then
		die "age identity not readable at $IDENTITY: mount your key there or pass -i PATH (or -i - to read it from stdin)"
	fi
}

# start_identity: sets AGE_ID, the path to pass to age -i.
start_identity() {
	if [ -z "$IDENTITY_KEY" ]; then
		AGE_ID=$IDENTITY
		return 0
	fi
	AGE_ID="$WORKDIR/identity.fifo"
	[ -p "$AGE_ID" ] || mkfifo "$AGE_ID" || die "cannot create a fifo"
	printf '%s\n' "$IDENTITY_KEY" >"$AGE_ID" &
	ID_PID=$!
}

stop_identity() {
	if [ -n "$ID_PID" ]; then
		kill "$ID_PID" 2>/dev/null
		wait "$ID_PID" 2>/dev/null
	fi
	ID_PID=
}

# decrypt_failed: die with age's first error line.
decrypt_failed() {
	die "$1: decryption failed (wrong identity?): $(head -n 1 "$WORKDIR/age.err")"
}

# fetch NAME: downloads NAME (ciphertext) and its manifest and checks the
# ciphertext sha256 against the manifest. Sets CIPHER and MANIFEST.
fetch() {
	path=$(object_path "$1")
	CIPHER="$WORKDIR/$1"
	MANIFEST="$CIPHER.json"
	info "$1: downloading"
	rcl copyto "$path.json" "$MANIFEST" || die "$1: manifest not found or not readable"
	rcl copyto "$path" "$CIPHER" || die "$1: backup file not found or not readable"
	want_sha=$(manifest_field "$MANIFEST" sha256)
	[ -n "$want_sha" ] || die "$1: the manifest has no sha256"
	got_sha=$(sha256sum "$CIPHER" | cut -d' ' -f1)
	if [ "$got_sha" != "$want_sha" ]; then
		die "$1: sha256 mismatch (manifest $want_sha, file $got_sha): the file is damaged or was changed"
	fi
	info "$1: sha256 ok"
}

# check_archive NAME: streams the decrypted ciphertext into pg_restore --list.
# Sets TOC_TABLES and TOC_ENTRIES.
check_archive() {
	fifo="$WORKDIR/plain.check"
	mkfifo "$fifo" || die "cannot create a fifo"
	start_identity
	age -d -i "$AGE_ID" "$CIPHER" >"$fifo" 2>"$WORKDIR/age.err" &
	age_pid=$!
	res=$(toc_check "$fifo")
	wait "$age_pid"
	age_rc=$?
	stop_identity
	rm -f "$fifo"
	[ "$age_rc" -eq 0 ] || decrypt_failed "$1"
	# shellcheck disable=SC2086 # "rc tables entries"
	set -- "$1" $res
	[ "$2" = 0 ] || die "$1: not a valid pg_dump archive: $(head -n 1 "$WORKDIR/toc.err")"
	TOC_TABLES=$3
	TOC_ENTRIES=$4
}

restore_setup() {
	require_storage
	umask 077
	make_workdir
	trap 'stop_identity; cleanup_workdir' EXIT
	trap 'stop_identity; cleanup_workdir; exit 130' INT TERM
	load_identity
}

cmd_verify() {
	parse_restore_args "$@"
	[ "$FORCE" = false ] && [ -z "$TO_FILE" ] || die "verify takes only a backup name and -i"
	restore_setup
	name=$(resolve_name "$WANT") || exit 1
	fetch "$name"
	check_archive "$name"
	info "$name: ok, $TOC_TABLES tables, $TOC_ENTRIES archive entries, database $(manifest_field "$MANIFEST" database), server $(manifest_field "$MANIFEST" server_version), schema version $(manifest_field "$MANIFEST" goose_version)"
}

restore_to_file() {
	start_identity
	age -d -i "$AGE_ID" -o "$TO_FILE" "$CIPHER" 2>"$WORKDIR/age.err"
	age_rc=$?
	stop_identity
	if [ "$age_rc" -ne 0 ]; then
		rm -f "$TO_FILE"
		decrypt_failed "$1"
	fi
	res=$(toc_check "$TO_FILE")
	# shellcheck disable=SC2086 # "rc tables entries"
	set -- "$1" $res
	if [ "$2" != 0 ]; then
		rm -f "$TO_FILE"
		die "$1: not a valid pg_dump archive: $(head -n 1 "$WORKDIR/toc.err")"
	fi
	info "$1: decrypted dump written to $TO_FILE ($3 tables, $4 archive entries); it is plaintext, delete it when done"
	info "restore it with: pg_restore --no-owner --no-privileges --exit-on-error -d DATABASE $TO_FILE"
}

cmd_restore() {
	parse_restore_args "$@"
	restore_setup
	name=$(resolve_name "$WANT") || exit 1

	if [ -n "$TO_FILE" ]; then
		fetch "$name"
		restore_to_file "$name"
		return 0
	fi

	# Check the target before downloading anything.
	setup_db
	tables=$(sql "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'" 2>"$WORKDIR/psql.err") ||
		die "cannot connect to the target database: $(tail -n 1 "$WORKDIR/psql.err")"
	if [ "$tables" -gt 0 ] && [ "$FORCE" != true ]; then
		die "the target database already has $tables table(s) in schema public; refusing to restore over them (use --force to drop and replace the objects in the backup)"
	fi

	fetch "$name"
	check_archive "$name"

	if [ "$FORCE" = true ] && [ "$tables" -gt 0 ]; then
		info "$name: restoring $TOC_TABLES tables with --clean (replacing $tables existing table(s))"
		set -- --clean --if-exists
	else
		info "$name: restoring $TOC_TABLES tables into an empty database"
		set --
	fi
	fifo="$WORKDIR/plain.restore"
	mkfifo "$fifo" || die "cannot create a fifo"
	start_identity
	age -d -i "$AGE_ID" "$CIPHER" >"$fifo" 2>"$WORKDIR/age.err" &
	age_pid=$!
	pg_restore --no-owner --no-privileges --exit-on-error --single-transaction "$@" \
		-d "$DB_NAME" <"$fifo" 2>"$WORKDIR/restore.err"
	restore_rc=$?
	wait "$age_pid"
	age_rc=$?
	stop_identity
	rm -f "$fifo"
	# A failing pg_restore stops reading, so age then dies of SIGPIPE with an
	# empty age.err: blame age only when the restore succeeded or age said why.
	if [ "$age_rc" -ne 0 ] && { [ "$restore_rc" -eq 0 ] || [ -s "$WORKDIR/age.err" ]; }; then
		decrypt_failed "$name"
	fi
	if [ "$restore_rc" -ne 0 ]; then
		err "$name: pg_restore failed; nothing was changed (single transaction)"
		tail -n 5 "$WORKDIR/restore.err" >&2
		exit 1
	fi
	tables=$(sql "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'") || tables='?'
	goose=$(manifest_field "$MANIFEST" goose_version)
	info "$name: restored ($tables tables in schema public, schema version ${goose:-unknown})"
	status_restored || true
	info "start the app next: it applies any newer migrations at startup"
}
