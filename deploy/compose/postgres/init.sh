#!/usr/bin/env bash
# Bootstraps the shogun database: extensions plus one schema and login role per
# service. Mounted into /docker-entrypoint-initdb.d of the pgvector/postgres
# image; init.sql and service.sql are read from SHOGUN_INIT_DIR.
# Every <NAME>_DB_PASSWORD variable (for example KAGAMI_DB_PASSWORD) defines one
# service, so adding a service needs no change here.
set -euo pipefail

mapfile -t SERVICES < <(compgen -e | sed -n 's/_DB_PASSWORD$//p' | tr '[:upper:]' '[:lower:]' | sort)
if [ "${#SERVICES[@]}" -eq 0 ]; then
  echo "init.sh: no <NAME>_DB_PASSWORD variables set" >&2
  exit 1
fi
INIT_DIR="${SHOGUN_INIT_DIR:-/opt/shogun/postgres}"

psql_admin() {
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "${POSTGRES_DB:-$POSTGRES_USER}" "$@"
}

psql_admin -f "$INIT_DIR/init.sql"

for svc in "${SERVICES[@]}"; do
  var="$(printf '%s' "$svc" | tr '[:lower:]' '[:upper:]')_DB_PASSWORD"
  password="${!var:-}"
  if [ -z "$password" ]; then
    echo "init.sh: $var is required" >&2
    exit 1
  fi
  psql_admin -v "svc=$svc" -v "pw=$password" -f "$INIT_DIR/service.sql"
done
