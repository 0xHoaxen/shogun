#!/usr/bin/env bash
# Tests for check-logs-clean.sh. Run: scripts/check-logs-clean_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
script="$here/check-logs-clean.sh"
failed=0

check() { # name, want exit code, input, args...
  local name=$1 want=$2 input=$3
  shift 3
  local got
  printf '%s' "$input" | "$script" "$@" >/dev/null 2>&1
  got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want"
    failed=1
  else
    echo "ok   $name"
  fi
}

check "clean logs pass" 0 $'{"msg":"sent","draft_id":"d1"}\n' "jobs@lumen.example" "secret-body"
check "a leaked address fails" 1 $'{"msg":"sent","to":"jobs@lumen.example"}\n' "jobs@lumen.example"
check "a leak among several strings fails" 1 $'{"token":"abc-123"}\n' "nothing" "abc-123"
check "matching is literal, not a regex" 0 $'{"msg":"axb"}\n' "a.b"
check "empty logs are an error, not a pass" 2 "" "x"
check "no strings is a usage error" 2 $'{"msg":"x"}\n'

exit $failed
