# Panvex backup and restore runbook

## SQLite — online backup

The `backup` subcommand uses SQLite's `VACUUM INTO` to capture a
consistent snapshot while the panel is running.  No downtime required.

```bash
panvex-control-plane backup \
  -storage-driver sqlite \
  -storage-dsn /var/lib/panvex/panvex.db \
  -out /var/lib/panvex/backups/panvex-$(date +%Y%m%dT%H%M%S).tar.gz
```

The archive (`panvex-<timestamp>.tar.gz`) contains:

| Entry           | Contents                                                     |
|-----------------|--------------------------------------------------------------|
| `panvex.db`     | Consistent SQLite snapshot (VACUUM INTO copy)                |
| `metadata.json` | Version, schema version, storage driver, encryption-key fingerprint, timestamp |

### metadata.json fields

```json
{
  "format_version": 1,
  "panel_version": "v1.2.3",
  "panel_commit": "abcdef12",
  "storage_driver": "sqlite",
  "schema_version": 50,
  "encryption_key_fingerprint": "1a2b3c4d",
  "created_at": "2026-06-10T10:00:00Z"
}
```

`encryption_key_fingerprint` is the first 8 hex characters of
`SHA256(PANVEX_ENCRYPTION_KEY)`.  It is one-way: storing the archive
alongside the database does not leak the key.  An empty fingerprint
means the backup was taken without `PANVEX_ENCRYPTION_KEY`.

---

## Verify before restore

Always verify an archive before touching a live database:

```bash
# Verify key fingerprint only (no DB access required):
panvex-control-plane restore -archive /var/lib/panvex/backups/panvex-20260610T100000.tar.gz

# Verify fingerprint AND schema version against the target DB:
panvex-control-plane restore \
  -archive /var/lib/panvex/backups/panvex-20260610T100000.tar.gz \
  -storage-driver sqlite \
  -storage-dsn /var/lib/panvex/panvex.db
```

### Fingerprint mismatch

```
restore: encryption-key fingerprint mismatch: archive 1a2b3c4d, current env 9e8f7a6b
```

The archive was taken with a different `PANVEX_ENCRYPTION_KEY`.
Restoring with the wrong key leaves all webhook/integration secrets
undecryptable — agents will fail to connect.  Locate the key that was
active at backup time before proceeding.

### Schema version mismatch

**Archive ahead of target** (`archive v50 > target v48`):
Safe to restore.  After placing the DB file, run `migrate-schema` to
bring the target forward (step 5 of the manual procedure below).

**Archive behind target** (`archive v48 < target v50`):
Goose cannot downgrade.  Restore onto a fresh empty path and let
`migrate-schema` bring it forward; do not overwrite the newer DB.

---

## SQLite — manual restore procedure

`panvex-control-plane restore` prints this procedure.  We deliberately
do not auto-restore: overwriting a populated DB silently loses fleets.

```
1.  Stop the panel:
      systemctl stop panvex-control-plane

2.  Extract the archive next to (NOT over) the existing DB:
      mkdir -p /var/lib/panvex/restore
      tar -xzf <archive>.tar.gz -C /var/lib/panvex/restore

3.  Inspect metadata.json — confirm panel_version and
    encryption_key_fingerprint match the panel you are restoring into.
    A mismatched fingerprint means PANVEX_ENCRYPTION_KEY is wrong;
    the restored DB will refuse to decrypt secrets.

4.  Move the snapshot into place (the old DB is preserved as .bak):
      mv /var/lib/panvex/panvex.db /var/lib/panvex/panvex.db.bak
      mv /var/lib/panvex/restore/panvex.db /var/lib/panvex/panvex.db

5.  Run any pending schema migrations:
      panvex-control-plane migrate-schema \
        -storage-driver sqlite \
        -storage-dsn /var/lib/panvex/panvex.db

6.  Start the panel:
      systemctl start panvex-control-plane
```

---

## PostgreSQL — backup

The `backup` subcommand refuses Postgres (it would produce an incomplete
archive).  Use `pg_dump` instead:

```bash
# Record these values BEFORE taking the dump — you need them at restore time.
SCHEMA_VERSION=$(psql "$PANVEX_STORAGE_DSN" \
  -t -c "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1" \
  | tr -d ' ')
KEY_FINGERPRINT=$(echo -n "$PANVEX_ENCRYPTION_KEY" | sha256sum | cut -c1-8)

# Take the dump (custom format — required for pg_restore --clean):
pg_dump \
  --format=custom \
  --file=/var/lib/panvex/backups/panvex-$(date +%Y%m%dT%H%M%S).pgdump \
  "$PANVEX_STORAGE_DSN"

# Store metadata alongside the dump:
cat > /var/lib/panvex/backups/panvex-$(date +%Y%m%dT%H%M%S)-metadata.json <<EOF
{
  "schema_version": $SCHEMA_VERSION,
  "encryption_key_fingerprint": "$KEY_FINGERPRINT",
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
```

### PostgreSQL — manual restore procedure

```
1.  Stop the panel:
      systemctl stop panvex-control-plane

2.  Verify the metadata.json you saved alongside the dump:
    - Compare encryption_key_fingerprint against the current
      PANVEX_ENCRYPTION_KEY (first 8 chars of SHA256).
    - Compare schema_version against the target DB:
        psql "$PANVEX_STORAGE_DSN" \
          -c "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1"
      If the dump is OLDER than the target, restore onto a fresh DB and
      let migrate-schema bring it forward.

3.  Restore the dump (--clean drops existing objects before recreating):
      pg_restore \
        --clean \
        --if-exists \
        --no-owner \
        --no-privileges \
        --dbname="$PANVEX_STORAGE_DSN" \
        /var/lib/panvex/backups/panvex-<timestamp>.pgdump

4.  Run any pending schema migrations:
      panvex-control-plane migrate-schema \
        -storage-driver postgres \
        -storage-dsn "$PANVEX_STORAGE_DSN"

5.  Start the panel:
      systemctl start panvex-control-plane
```

---

## Schedule recommendation

| Frequency | Retention | Location                        |
|-----------|-----------|---------------------------------|
| Daily     | 7 copies  | Off-host (object storage / NFS) |
| Weekly    | 4 copies  | Off-host                        |

**Quarterly restore test**: restore the most recent weekly backup onto a
staging instance, run `panvex-control-plane restore -archive <path> -storage-driver sqlite -storage-dsn <staging-db>` to confirm verification passes, then start the panel and
verify agents reconnect and secrets decrypt correctly.
