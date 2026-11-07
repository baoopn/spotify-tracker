#!/bin/sh
# Fails if tracked files contain things that must never be published:
# machine-specific paths, real-looking API keys or tokens, private key blocks,
# deployment details (Coolify host, resource UUIDs, server IPs), personal
# mailboxes, a committed .env/.secrets, or ruflo runtime state.
# Run in CI ("Public-safety check") and before pushing.
set -eu

fail=0

# check LABEL PATTERN [EXCLUDE]
#   PATTERN and EXCLUDE are extended regexes; EXCLUDE filters "file:line:text"
#   hits (e.g. placeholders and third-party files that legitimately match).
check() {
	label=$1
	pattern=$2
	exclude=${3:-'^$'}
	hits=$(git ls-files -z | xargs -0 grep -I -n -E -- "$pattern" 2>/dev/null |
		grep -v -E '^scripts/check-public\.sh:' | grep -v -E -- "$exclude" || true)
	if [ -n "$hits" ]; then
		echo "::error::$label"
		echo "$hits" | cut -c1-160
		fail=1
	fi
}

check "machine-specific paths" \
	'[A-Za-z]:[\\/]+(Users|Work)[\\/]|/[a-z]/(Users|Work)/|/home/[a-z]+/|/Users/[A-Za-z]+/' \
	'^\.claude/(skills|commands|agents|helpers)/|/home/(user|runner|nonroot)/|/Users/(you|username)/'
check "Clerk secret keys" 'sk_(live|test)_[A-Za-z0-9]{20,}'
check "Clerk live publishable keys" 'pk_live_[A-Za-z0-9]{20,}'
check "Clerk instance/app/user IDs" '\b(ins|app|user|org)_[A-Za-z0-9]{24,}\b'
check "useSend API keys" '\bus_[A-Za-z0-9]{24,}'
check "webhook secrets" 'whsec_[A-Za-z0-9+/=]{16,}'
check "GitHub tokens" '\b(ghp|gho|ghs|ghu|github_pat)_[A-Za-z0-9_]{20,}'
check "Cloudflare API tokens" '(CF_API_TOKEN|CLOUDFLARE_API_TOKEN)[=: ]+[A-Za-z0-9_-]{30,}'
check "Spotify / Soundcharts credentials" \
	'(SPOTIFY_CLIENT_SECRET|SPOTIFY_REFRESH_TOKEN|SOUNDCHARTS_TOKEN|SECRETS_KEY|INTERNAL_API_TOKEN)[=: ]+[A-Za-z0-9_-]{20,}' \
	'change-me'
check "private key blocks" '-----BEGIN [A-Z ]*PRIVATE KEY-----'
# Deployment details: only placeholder Coolify hosts, no resource UUIDs.
check "Coolify host" 'coolify\.[a-z0-9-]+\.[a-z]{2,}' 'coolify\.example\.(com|org)'
check "Coolify resource UUIDs" \
	'(uuid|UUID|applications|services|projects|servers)[/=:"'"'"' ]+[a-z0-9]{24}\b'
# Public IPv4 addresses (loopback, unspecified, private ranges and dotted
# version strings such as Chrome/141.0.0.0 are fine).
check "server IP addresses" \
	'(^|[^0-9A-Za-z./_-])([0-9]{1,3}\.){3}[0-9]{1,3}([^0-9.]|$)' \
	'(^|[^0-9.])(127\.|0\.0\.0\.0|10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.)|^\.claude/|^go\.sum:|^web/[a-z]+/pnpm-lock\.yaml:|[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+\.svg| d="M'
# Real mailboxes (placeholders use example.com and friends).
check "personal email addresses" \
	'[A-Za-z0-9._%+-]+@(gmail|googlemail|outlook|hotmail|live|icloud|me|yahoo|proton|protonmail)\.[a-z]{2,}' \
	'^\.claude/skills/'

if git ls-files | grep -E '(^|/)\.env$|(^|/)\.secrets/' >/dev/null; then
	echo "::error::.env or .secrets/ is tracked"
	git ls-files | grep -E '(^|/)\.env$|(^|/)\.secrets/'
	fail=1
fi

runtime='^\.claude-flow/(data|logs|sessions|metrics|policy|swarm|security)/|^\.claude-flow/(memory-package|harness-active-policy)\.json$|^\.swarm/|^\.hive-mind/|^\.claude/(settings\.local\.json|proven-config\.json|\.proven-config-version)$|(^|/)\.pnpm-store/'
if git ls-files | grep -E "$runtime" >/dev/null; then
	echo "::error::machine/runtime state is tracked (ruflo state, pnpm store)"
	git ls-files | grep -E "$runtime" | head -20
	fail=1
fi

[ "$fail" -eq 0 ] && echo "public-safety check: OK"
exit "$fail"
