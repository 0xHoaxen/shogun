#!/usr/bin/env bash
# Throwaway check of init.sh against a real pgvector Postgres 17 container.
# Usage: deploy/compose/postgres/test-init.sh
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
NAME="shogun-pg-init-test-$$"
IMAGE="pgvector/pgvector:pg17"
ENV_ARGS=(-e POSTGRES_PASSWORD=admin)
for svc in torii kagami tsubame fude taiko dojo katana shinobi sensei soroban; do
  ENV_ARGS+=(-e "$(printf '%s' "$svc" | tr '[:lower:]' '[:upper:]')_DB_PASSWORD=pw-$svc")
done

cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run -d --name "$NAME" "${ENV_ARGS[@]}" \
  -v "$HERE/init.sh:/docker-entrypoint-initdb.d/00-shogun.sh:ro" \
  -v "$HERE/init.sql:/opt/shogun/postgres/init.sql:ro" \
  -v "$HERE/service.sql:/opt/shogun/postgres/service.sql:ro" \
  "$IMAGE" >/dev/null

# The entrypoint starts a temporary server for init scripts, then restarts, so
# wait for the final "ready to accept connections" after init has finished.
for _ in $(seq 1 60); do
  if docker logs "$NAME" 2>&1 | grep -q "PostgreSQL init process complete" &&
    docker exec "$NAME" pg_isready -U postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

as() { docker exec -e PGPASSWORD="pw-$1" "$NAME" psql -v ON_ERROR_STOP=1 -h localhost -U "$1" -d postgres -tA -c "$2"; }
expect_denied() {
  if out=$(as "$1" "$2" 2>&1); then
    echo "FAIL: expected permission error for $1: $2" >&2
    echo "$out" >&2
    exit 1
  fi
  echo "ok (denied): $1: $2 -> $(printf '%s' "$out" | head -1)"
}

echo "== kagami sees search_path and its own schema"
as kagami "SHOW search_path"
as kagami "CREATE TABLE kagami.t (id int)"
as kagami "SELECT count(*) FROM kagami.t"
echo "== kagami cannot touch fude, public"
expect_denied kagami "CREATE TABLE fude.t (id int)"
expect_denied kagami "CREATE TABLE public.t (id int)"
expect_denied kagami "SELECT 1 FROM fude.t"
echo "== extension types and operators usable from a service role (schema-qualified)"
# pkg/postgres.Connect pins search_path to the service schema only, so operators
# in "extensions" must be qualified until the follow-up task P3.2b lands.
as fude "SELECT '[1,2,3]'::extensions.vector OPERATOR(extensions.<->) '[1,2,4]'::extensions.vector"
as kagami "SELECT 'A@B.com'::extensions.citext OPERATOR(extensions.=) 'a@b.com'::extensions.citext"
echo "== every service role exists with its own schema"
docker exec -e PGPASSWORD=admin "$NAME" psql -h localhost -U postgres -tA \
  -c "SELECT count(*) FROM pg_roles r JOIN pg_namespace n ON n.nspname = r.rolname AND n.nspowner = r.oid"
echo "PASS"
