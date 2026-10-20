#!/usr/bin/env bash
# Tests check-owner-scope.sh on small fixture repos. Run: scripts/check-owner-scope_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
script="$here/check-owner-scope.sh"
failed=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

check() {
	if eval "$2"; then echo "ok   $1"; else echo "FAIL $1"; failed=1; fi
}

# fixture NAME SQL writes services/svc/internal/store/queries/NAME.sql in a fresh repo.
fixture() {
	local repo="$work/$1"
	mkdir -p "$repo/services/svc/internal/store/queries"
	printf '%s\n' "$2" >"$repo/services/svc/internal/store/queries/q.sql"
	echo "$repo"
}

repo=$(fixture scoped '-- name: GetJob :one
SELECT * FROM jobs WHERE owner_id = @owner_id AND id = @id;')
check "a query filtering on owner_id passes" '"$script" "$repo" 2>/dev/null'

repo=$(fixture unscoped '-- name: GetJob :one
SELECT * FROM jobs WHERE id = @id;')
check "a query without owner_id fails" '! "$script" "$repo" 2>/dev/null'
check "the failure names the query" '"$script" "$repo" 2>&1 | grep -q "GetJob"'

repo=$(fixture annotated '-- name: ListJobEvents :many
-- owner-scope: child only after GetJob(owner, id)
SELECT * FROM job_events WHERE job_id = @job_id;')
check "an annotated query passes" '"$script" "$repo" 2>/dev/null'

repo=$(fixture noreason '-- name: ListJobEvents :many
-- owner-scope: child
SELECT * FROM job_events WHERE job_id = @job_id;')
check "an annotation without a reason fails" '! "$script" "$repo" 2>/dev/null'

repo=$(fixture unknownkind '-- name: ListJobEvents :many
-- owner-scope: whatever because
SELECT * FROM job_events WHERE job_id = @job_id;')
check "an unknown annotation kind fails" '! "$script" "$repo" 2>/dev/null'

repo=$(fixture second '-- name: A :one
SELECT 1 WHERE owner_id = @owner_id;

-- name: B :one
SELECT 1;')
check "only the unscoped one of two queries is flagged" '"$script" "$repo" 2>&1 | grep -c "B has no owner_id" | grep -qx 1 && ! "$script" "$repo" 2>&1 | grep -q " A has"'

check "the real repo passes" '"$script" 2>/dev/null'

exit "$failed"
