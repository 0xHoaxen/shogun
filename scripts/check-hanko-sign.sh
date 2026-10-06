#!/usr/bin/env bash
# Fails unless hanko.Sign is called from exactly one file: fude's approve.go.
# Only Approve may stamp a Hanko (AGENTS.md rule 4). Tests and pkg/hanko itself
# are exempt. Usage: scripts/check-hanko-sign.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
allowed="services/fude/internal/app/approve.go"

cd "$root"
found="$(grep -rl --include='*.go' --exclude='*_test.go' --exclude-dir=hanko --exclude-dir=node_modules --exclude-dir=.git 'hanko\.Sign(' . | sed 's|^\./||' | sort)"

if [[ "$found" != "$allowed" ]]; then
	echo "hanko.Sign must be called only from $allowed, but is called from:" >&2
	echo "${found:-<nowhere>}" >&2
	exit 1
fi
echo "ok: hanko.Sign is referenced only from $allowed"
