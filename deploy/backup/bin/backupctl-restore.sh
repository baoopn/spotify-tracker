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

# load_identity: copies the identity (file or stdin) into the work dir.
load_identity() {
	IDENTITY_FILE="$WORKDIR/identity"
	if [ "$IDENTITY" = - ]; then
		cat >"$IDENTITY_FILE" || die "cannot read the identity from stdin"
	elif [ -r "$IDENTITY" ]; then
		cat "$IDENTITY" >"$IDENTITY_FILE" || die "cannot read the identity file"
	else
		die "age identity not readable at $IDENTITY: mount your key there or pass -i PATH (or -i - to read it from stdin)"
	fi
	grep -q '^AGE-SECRET-KEY-' "$IDENTITY_FILE" || grep -q 'PRIVATE KEY' "$IDENTITY_FILE" ||
		die "the identity does not look like an age key (AGE-SECRET-KEY-...)"
}

# fetch_and_decrypt NAME: downloads NAME and its manifest, checks the
# ciphertext sha256 against the manifest, decrypts to $PLAIN and checks that
# the result is a pg_dump archive.
fetch_and_decrypt() {
	name=$1
	path=$(object_path "$name")
	cipher="$WORKDIR/$name"
	MANIFEST="$cipher.json"
	PLAIN="$WORKDIR/restore.dump"

	info "$name: downloading"
	rcl copyto "$path.json" "$MANIFEST" || die "$name: manifest not found or not readable"
	rcl copyto "$path" "$cipher" || die "$name: backup file not found or not readable"

	want_sha=$(manifest_field "$MANIFEST" sha256)
	[ -n "$want_sha" ] || die "$name: the manifest has no sha256"
	got_sha=$(sha256sum "$cipher" | cut -d' ' -f1)
	if [ "$got_sha" != "$want_sha" ]; then
		die "$name: sha256 mismatch (manifest $want_sha, file $got_sha): the file is damaged or was changed"
	fi
	info "$name: sha256 ok"

	info "$name: decrypting"
	if ! age -d -i "$IDENTITY_FILE" -o "$PLAIN" "$cipher" 2>"$WORKDIR/age.err"; then
		die "$name: decryption failed (wrong identity?): $(head -n 1 "$WORKDIR/age.err")"
	fi
	rm -f "$cipher"
	if ! pg_restore --list "$PLAIN" >"$WORKDIR/toc" 2>"$WORKDIR/toc.err"; then
		die "$name: not a valid pg_dump archive: $(head -n 1 "$WORKDIR/toc.err")"
	fi
	TOC_ENTRIES=$(grep -c '^[0-9]' "$WORKDIR/toc")
}

restore_setup() {
	require_storage
	umask 077
	make_workdir
	trap 'cleanup_workdir' EXIT
	trap 'cleanup_workdir; exit 130' INT TERM
	load_identity
}

cmd_verify() {
	parse_restore_args "$@"
	[ "$FORCE" = false ] && [ -z "$TO_FILE" ] || die "verify takes only a backup name and -i"
	restore_setup
	name=$(resolve_name "$WANT") || exit 1
	fetch_and_decrypt "$name"
	info "$name: ok, $TOC_ENTRIES archive entries, database $(manifest_field "$MANIFEST" database), server $(manifest_field "$MANIFEST" server_version), schema version $(manifest_field "$MANIFEST" goose_version)"
}

cmd_restore() {
	parse_restore_args "$@"
	restore_setup
	name=$(resolve_name "$WANT") || exit 1

	if [ -z "$TO_FILE" ]; then
		# Check the target before downloading anything.
		setup_db
		tables=$(sql "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'" 2>"$WORKDIR/psql.err") ||
			die "cannot connect to the target database: $(tail -n 1 "$WORKDIR/psql.err")"
		if [ "$tables" -gt 0 ] && [ "$FORCE" != true ]; then
			die "the target database already has $tables table(s) in schema public; refusing to restore over them (use --force to drop and replace the objects in the backup)"
		fi
	fi

	fetch_and_decrypt "$name"

	if [ -n "$TO_FILE" ]; then
		# Keep the umask: the dump holds the plaintext data.
		mv "$PLAIN" "$TO_FILE" || die "cannot write $TO_FILE"
		info "$name: decrypted dump written to $TO_FILE ($TOC_ENTRIES archive entries)"
		info "restore it with: pg_restore --no-owner --no-privileges --exit-on-error -d DATABASE $TO_FILE"
		return 0
	fi

	if [ "$FORCE" = true ] && [ "$tables" -gt 0 ]; then
		info "$name: restoring with --clean (replacing $tables existing table(s))"
		set -- --clean --if-exists
	else
		info "$name: restoring into an empty database"
		set --
	fi
	if ! pg_restore --no-owner --no-privileges --exit-on-error --single-transaction "$@" \
		-d "$DB_TARGET" "$PLAIN" 2>"$WORKDIR/restore.err"; then
		err "$name: pg_restore failed; nothing was changed (single transaction)"
		tail -n 5 "$WORKDIR/restore.err" >&2
		exit 1
	fi
	tables=$(sql "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'") || tables='?'
	goose=$(manifest_field "$MANIFEST" goose_version)
	info "$name: restored ($tables tables in schema public, schema version ${goose:-unknown})"
	info "start the app next: it applies any newer migrations at startup"
}
