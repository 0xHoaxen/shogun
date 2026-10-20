#!/usr/bin/env bash
# Every sqlc query in a service must filter on owner_id (compare it or bind @owner_id;
# naming it in ORDER BY or the select list does not count), or say why it does not
# with a comment right under its "-- name:" line:
#
#   -- owner-scope: child <why>     reached only through an owner-checked parent row
#   -- owner-scope: internal <why>  called by a job or event handler, never by an RPC with a user-supplied id
#   -- owner-scope: global <why>    not user data (prices, session tokens)
#
# A new query without either fails make lint, so a cross-tenant read cannot slip in.
set -euo pipefail

root="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
status=0

for file in "$root"/services/*/internal/store/queries/*.sql; do
	[ -e "$file" ] || continue
	offenders=$(awk '
		function flush() {
			if (name != "" && !(scoped || annotated)) print name
		}
		/^-- name:/ { flush(); name = $3; scoped = 0; annotated = 0; next }
		/^-- owner-scope: (child|internal|global) [^ ]/ { annotated = 1; next }
		/@owner_id|owner_id[ ]*(=|<>|IN )/ { scoped = 1 }
		END { flush() }
	' "$file")
	if [ -n "$offenders" ]; then
		while IFS= read -r query; do
			echo "check-owner-scope: ${file#"$root"/}: $query has no owner_id filter and no '-- owner-scope:' note" >&2
		done <<<"$offenders"
		status=1
	fi
done

exit "$status"
