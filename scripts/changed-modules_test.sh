#!/usr/bin/env bash
# Tests for changed-modules.sh. Usage: scripts/changed-modules_test.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
script="$root/scripts/changed-modules.sh"
failures=0

all="$(cd "$root" && go list -m -f '{{.Dir}}' | sed "s|^$root/||" | awk 'BEGIN{printf "{\"dir\":["} {printf "%s\"%s\"", (NR>1?",":""), $0} END{print "]}"}')"

check() { # name, changed files, expected json
	local got
	got="$(CHANGED_FILES="$2" "$script" HEAD)"
	if [[ "$got" == "$3" ]]; then
		echo "ok   $1"
	else
		echo "FAIL $1"
		echo "  want: $3"
		echo "  got:  $got"
		failures=$((failures + 1))
	fi
}

check "service file selects only that service" $'services/kagami/x.go' '{"dir":["services/kagami"]}'
check "two services select both once" $'services/kagami/a.go\nservices/fude/b.go\nservices/kagami/c.go' '{"dir":["services/fude","services/kagami"]}'
check "pkg change selects all" $'pkg/config/x.go' "$all"
check "gen change selects all" $'gen/go/shogun/events/v1/x.pb.go' "$all"
check "proto change selects all" $'proto/shogun/kagami/v1/kagami.proto' "$all"
check "go.work change selects all" $'go.work' "$all"
check "service plus pkg selects all" $'services/kagami/x.go\npkg/config/x.go' "$all"
check "docs only selects nothing" $'README.md\ndocs/folder-structure.md' '{"dir":[]}'
check "no changes selects nothing" '' '{"dir":[]}'
check "service prefix is not a substring match" $'services/kagami-extra/x.go' '{"dir":[]}'

if ((failures > 0)); then
	echo "$failures failed" >&2
	exit 1
fi
echo "PASS"
