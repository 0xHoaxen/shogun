#!/usr/bin/env bash
# Print "true" when every non-merge commit in <base>..<head> has a Conventional
# Commits "ci" type (ci:, ci(scope):, ci!:), otherwise "false". Workflows use it
# to skip work for pushes that only touch CI. An empty range prints "false".
# Usage: scripts/ci-only-commits.sh <base-ref> [head-ref]
# SUBJECTS (newline separated) replaces the git log, which is how the test drives it.
set -euo pipefail

if [[ -n "${SUBJECTS+x}" ]]; then
	subjects="$SUBJECTS"
else
	base="${1:?usage: ci-only-commits.sh <base-ref> [head-ref]}"
	head="${2:-HEAD}"
	subjects="$(git log --no-merges --format=%s "$base..$head")"
fi

if [[ -n "$subjects" ]] && ! grep -qvE '^ci(\([^)]+\))?!?:' <<<"$subjects"; then
	echo true
else
	echo false
fi
