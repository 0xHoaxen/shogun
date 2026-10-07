#!/usr/bin/env bash
# Nightly backup of the shogun database.
#
#   backup.sh once    take one backup now and exit
#   backup.sh loop    take one every day at BACKUP_AT (Asia/Kolkata), forever
#
# Each run writes two files to BACKUP_DIR, named by UTC time:
#   shogun-<time>.dump         pg_dump custom format of the whole database
#   shogun-<time>.roles.sql    the login roles, with their password hashes
# A restore needs the roles first (docs/backup-restore.md). Files are written
# under a temporary name and renamed when complete, so a crash never leaves a
# half-written backup that looks finished. Backups older than RETENTION_DAYS are
# deleted after a successful run, never before.
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/backups}"
BACKUP_AT="${BACKUP_AT:-02:30}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
BACKUP_TZ="${BACKUP_TZ:-Asia/Kolkata}"
PGHOST="${PGHOST:-postgres}"
PGUSER="${PGUSER:-postgres}"
PGDATABASE="${PGDATABASE:-shogun}"
export PGHOST PGUSER PGDATABASE
# PGPASSWORD comes from the environment.

log() { printf '%s backup: %s\n' "$(date -u +%FT%TZ)" "$*"; }

backup_once() {
  local stamp base
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  base="$BACKUP_DIR/shogun-$stamp"
  mkdir -p "$BACKUP_DIR"

  # The postgres superuser exists in every server, so it is left out of the roles file.
  pg_dumpall --roles-only | grep -v -E '^(CREATE|ALTER) ROLE postgres[ ;]' >"$base.roles.sql.tmp"
  pg_dump --format=custom --file="$base.dump.tmp"

  # Both files must be real before either is published.
  [ -s "$base.roles.sql.tmp" ] || { log "the roles file is empty; keeping the old backups"; rm -f "$base".*.tmp; return 1; }
  [ -s "$base.dump.tmp" ] || { log "the dump is empty; keeping the old backups"; rm -f "$base".*.tmp; return 1; }
  mv "$base.roles.sql.tmp" "$base.roles.sql"
  mv "$base.dump.tmp" "$base.dump"
  log "wrote $base.dump ($(du -h "$base.dump" | cut -f1))"

  find "$BACKUP_DIR" -maxdepth 1 -type f \( -name 'shogun-*.dump' -o -name 'shogun-*.roles.sql' \) \
    -mtime "+$RETENTION_DAYS" -print -delete | sed 's/^/backup: pruned /'
}

seconds_until_next_run() {
  local now target
  now="$(date +%s)"
  target="$(TZ="$BACKUP_TZ" date -d "today $BACKUP_AT" +%s)"
  if [ "$target" -le "$now" ]; then
    target="$(TZ="$BACKUP_TZ" date -d "tomorrow $BACKUP_AT" +%s)"
  fi
  echo $((target - now))
}

case "${1:-}" in
  once)
    backup_once
    ;;
  loop)
    log "running daily at $BACKUP_AT $BACKUP_TZ, keeping $RETENTION_DAYS days"
    while true; do
      wait_for="$(seconds_until_next_run)"
      log "next backup in ${wait_for}s"
      sleep "$wait_for"
      # A failed backup is logged and tried again tomorrow; it must not stop the loop.
      backup_once || log "backup failed"
    done
    ;;
  *)
    echo "usage: backup.sh once|loop" >&2
    exit 2
    ;;
esac
