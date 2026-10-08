# Backup and restore

Shogun keeps everything in one Postgres database, `shogun`, with one schema and one login role per service. A backup is therefore two files:

| File | What it is |
| --- | --- |
| `shogun-<UTC time>.dump` | `pg_dump --format=custom` of the whole database: every schema, table, index, grant and the `vector` extension |
| `shogun-<UTC time>.roles.sql` | the service login roles with their password hashes (`pg_dumpall --roles-only`, without the `postgres` superuser) |

## How backups are taken

The `backup` service in `deploy/compose/compose.yaml` runs `deploy/compose/backup/backup.sh loop`. Every day at **02:30 India time** (after sensei's 01:00 rollup) it writes both files to the `backups` volume, under a temporary name first and renamed when complete, so a crash never leaves a half-written file that looks finished. After a successful run it deletes backups older than `BACKUP_RETENTION_DAYS` (default 14); a failed run deletes nothing.

Take one now: `docker compose exec backup /opt/shogun/backup.sh once`.
List them: `docker compose exec backup ls -lh /backups`.

**The `backups` volume lives on the same machine as the database.** It protects against a bad migration or an accidental delete, not against losing the machine. `TODO(owner)`: copy the newest pair somewhere else (another disk, object storage) on a schedule, and decide how many days to keep.

Backups contain every draft, contact and mail record in the database, and the encrypted OAuth tokens. Treat them like the database: do not commit them, and encrypt them before they leave the machine.

## Check that a backup restores

```sh
make up
scripts/restore-check.sh
```

The script takes a fresh backup with the running service, restores that exact file into a new throwaway Postgres container, and compares the **exact row count of every table in every schema** with the live database. It prints `restore-check: ok, N tables and M rows match` and exits 0, or prints the differing tables and exits 1. If rows change while it runs it stops with exit 2 instead of guessing; stop the services that write (`docker compose stop torii kagami ...`) and run it again. It removes its container on the way out.

Run it after changing the schema, and from time to time: a backup nobody has restored is a hope.

## Restore for real

1. **Stop everything that writes**: `docker compose stop` the services (keep `postgres` and `backup`).
2. **Pick the backup** and copy both files out of the volume:
   ```sh
   docker compose exec backup ls -1t /backups
   docker cp "$(docker compose ps -q backup)":/backups/shogun-<time>.dump .
   docker cp "$(docker compose ps -q backup)":/backups/shogun-<time>.roles.sql .
   ```
3. **Start an empty Postgres 17 with pgvector** (`pgvector/pgvector:pg17`, the image the stack uses) and copy the files into it. To restore over the existing database instead, first drop it: `DROP DATABASE shogun;` from a session on the `postgres` database.
4. **Restore the roles first**, because the dump grants privileges to them:
   ```sh
   psql -v ON_ERROR_STOP=1 -U postgres -d postgres -f shogun-<time>.roles.sql
   ```
   If a role already exists, the file errors on it; drop the role or edit that line out.
5. **Restore the database**, creating it from the dump:
   ```sh
   pg_restore --exit-on-error --create --dbname=postgres -U postgres shogun-<time>.dump
   ```
6. **Check it**: `psql -U postgres -d shogun -c '\dn'` lists the ten service schemas, and row counts can be compared with `scripts/restore-check.sh`'s query.
7. **Start the services.** They apply any newer migrations on start (`MIGRATE_ON_START`). Events written after the backup are gone; each service's outbox and inbox come back as they were, so events relay from where the backup left them.

What a restore does not bring back: anything that happened after the backup. Events written after it are gone, and so is `tsubame`'s record of any email it sent after it, so the single-use check can no longer stop a second send of that draft. Look at the mailbox's Sent folder before approving a draft again.

## Settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `BACKUP_RETENTION_DAYS` | `14` | days of backups kept (set in `.env`) |
| `BACKUP_AT`, `BACKUP_TZ` | `02:30`, `Asia/Kolkata` | when the daily backup runs (set on the service in compose) |
