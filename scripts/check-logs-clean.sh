#!/usr/bin/env bash
# Fails if any of the given strings appears in the log text on stdin.
#
#   docker compose logs --no-color kagami fude | scripts/check-logs-clean.sh "secret text" "other"
#
# Used after the compose end-to-end run: the strings are the message text,
# recipients and tokens the flows sent through the services, which must never
# reach a log (AGENTS.md rule 8). Matching is literal and case-sensitive.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: check-logs-clean.sh <string>..." >&2
  exit 2
fi

logs=$(cat)
if [ -z "$logs" ]; then
  echo "check-logs-clean: no log text on stdin, so nothing was checked" >&2
  exit 2
fi

leaked=0
for needle in "$@"; do
  if [ -z "$needle" ]; then
    continue
  fi
  count=$(grep -cF -- "$needle" <<<"$logs" || true)
  if [ "$count" -gt 0 ]; then
    echo "LEAK: ${count} log line(s) contain a forbidden string (${needle:0:3}... length ${#needle})" >&2
    leaked=1
  fi
done

if [ "$leaked" -ne 0 ]; then
  exit 1
fi
echo "logs are clean of $# forbidden string(s)"
