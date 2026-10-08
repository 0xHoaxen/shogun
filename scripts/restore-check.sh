#!/usr/bin/env bash
# Proves a backup restores: takes a backup with the running stack's backup
# service, restores that very file into a fresh container, and compares the exact
# row count of every table in every schema between the live database and the
# restored one. Exits 0 only if every table matches.
#
#   make up && scripts/restore-check.sh
#
# The stack should be idle while it runs; rows written between the counts and the
# dump would show up as a difference, and the script says so instead of guessing.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
env_file="${ENV_FILE:-$([ -f "$root/.env" ] && echo "$root/.env" || echo "$root/.env.example")}"
compose=(docker compose --env-file "$env_file" -f "$root/deploy/compose/compose.yaml")
image="${RESTORE_IMAGE:-pgvector/pgvector:pg17}"
restore_password="restore-check-$RANDOM$RANDOM"
workdir="$(mktemp -d)"
container="shogun-restore-check-$$"

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$workdir"
}
trap cleanup EXIT

# Exact count of every base table outside the system schemas, one per line.
counts_sql="SELECT table_schema || '.' || table_name || ' ' ||
  (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I', table_schema, table_name), false, true, '')))[1]::text
FROM information_schema.tables
WHERE table_type = 'BASE TABLE' AND table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1"

live_counts() { "${compose[@]}" exec -T postgres psql -qAt -U postgres -d shogun -c "$counts_sql"; }
restored_counts() { docker exec "$container" psql -qAt -U postgres -d shogun -c "$counts_sql"; }

echo "==> counting the live database"
live_before="$(live_counts)"
[ -n "$live_before" ] || { echo "restore-check: the live database has no tables" >&2; exit 2; }

echo "==> taking a backup with the backup service"
"${compose[@]}" exec -T backup /opt/shogun/backup.sh once
latest="$("${compose[@]}" exec -T backup sh -c 'ls -1t /backups/shogun-*.dump | head -n 1' | tr -d '\r')"
[ -n "$latest" ] || { echo "restore-check: no backup found in the backups volume" >&2; exit 2; }
roles="${latest%.dump}.roles.sql"
backup_container="$("${compose[@]}" ps -q backup)"
docker cp "$backup_container:$latest" "$workdir/shogun.dump"
docker cp "$backup_container:$roles" "$workdir/shogun.roles.sql"
echo "    $(basename "$latest")"

echo "==> counting the live database again"
live_after="$(live_counts)"
if [ "$live_before" != "$live_after" ]; then
  echo "restore-check: rows changed while the backup was taken; stop the services that write and run again" >&2
  diff <(echo "$live_before") <(echo "$live_after") >&2 || true
  exit 2
fi

echo "==> restoring into a fresh container"
docker run -d --name "$container" -e POSTGRES_PASSWORD="$restore_password" "$image" >/dev/null
for _ in $(seq 1 60); do
  docker exec "$container" pg_isready -q -U postgres && break
  sleep 1
done
docker exec "$container" pg_isready -q -U postgres || { echo "restore-check: the fresh database did not start" >&2; exit 2; }
docker cp "$workdir/shogun.roles.sql" "$container:/tmp/roles.sql"
docker cp "$workdir/shogun.dump" "$container:/tmp/shogun.dump"
docker exec "$container" psql -q -v ON_ERROR_STOP=1 -U postgres -d postgres -f /tmp/roles.sql >/dev/null
docker exec "$container" pg_restore --exit-on-error --create --dbname=postgres -U postgres /tmp/shogun.dump

echo "==> comparing row counts"
restored="$(restored_counts)"
if [ "$live_after" != "$restored" ]; then
  echo "restore-check: the restored database differs from the live one:" >&2
  diff <(echo "$live_after") <(echo "$restored") >&2 || true
  exit 1
fi
tables="$(echo "$restored" | wc -l | tr -d ' ')"
rows="$(echo "$restored" | awk '{ sum += $2 } END { print sum + 0 }')"
echo "restore-check: ok, $tables tables and $rows rows match"
