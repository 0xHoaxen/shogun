#!/usr/bin/env bash
# Tests for ci-only-commits.sh. Usage: scripts/ci-only-commits_test.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
script="$root/scripts/ci-only-commits.sh"
failures=0

check() { # name, subjects, expected
	local got
	got="$(SUBJECTS="$2" "$script")"
	if [[ "$got" == "$3" ]]; then
		echo "ok   $1"
	else
		echo "FAIL $1"
		echo "  want: $3"
		echo "  got:  $got"
		failures=$((failures + 1))
	fi
}

check "plain ci type" 'ci: tweak workflow' true
check "ci with scope" 'ci(deploy): pin action' true
check "ci breaking marker" 'ci(deploy)!: drop job' true
check "several ci commits" $'ci: a\nci(proto): b' true
check "ci mixed with feat" $'ci: a\nfeat(kagami): b' false
check "feat only" 'feat(kagami): import contacts' false
check "ci only in the description" 'fix: ci stuff' false
check "type merely starting with ci" 'cidr: x' false
check "empty range" '' false

if ((failures > 0)); then
	echo "$failures failed" >&2
	exit 1
fi
echo "PASS"
