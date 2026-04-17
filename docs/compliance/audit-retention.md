# Audit retention & compliance matrix

**Task:** P2-COMPLIANCE-01 (remediation plan v4)
**Related:** P2-REL-03 (configurable retention), ADR-003
**Status:** Reference document — operator guidance, not a runtime contract.

This document describes how Panvex's retention knobs map onto the common
regulatory regimes operators encounter, and lays out the data-subject-request
(DSR) workflow Panvex supports today for the EU "right to erasure" and the
similar provisions in other jurisdictions.

It does **not** constitute legal advice. Every deployment must reconcile
these recommendations with its own legal counsel, data-processing agreements,
and customer contracts before settling on values.

---

## 1. Retention is already configurable

Panvex persists retention as an opaque JSON blob in
`panel_settings.retention_json` (see
[ADR-003](../architecture/adr/003-retention-configurable-ttl.md)). The full
record is `storage.RetentionSettingsRecord` in
`internal/controlplane/storage/models.go`, and it exposes seven knobs:

| Field                       | JSON key                  | Controls                                      | Default (seconds) | Default (human) |
| --------------------------- | ------------------------- | --------------------------------------------- | ----------------- | --------------- |
| `TSRawSeconds`              | `ts_raw_seconds`          | Raw per-agent timeseries samples              | 86 400            | 24 h            |
| `TSHourlySeconds`           | `ts_hourly_seconds`       | Hourly rollups of the raw timeseries          | 2 592 000         | 30 d            |
| `TSDCSeconds`               | `ts_dc_seconds`           | Per-DC aggregated rollups                     | 86 400            | 24 h            |
| `IPHistorySeconds`          | `ip_history_seconds`      | Client IP history                             | 2 592 000         | 30 d            |
| `EventSeconds`              | `event_history_seconds`   | Runtime events (non-audit)                    | 86 400            | 24 h            |
| `AuditEventSeconds`         | `audit_event_seconds`     | Security-relevant audit trail                 | 7 776 000         | **90 d**        |
| `MetricSnapshotSeconds`     | `metric_snapshot_seconds` | Per-tick metric snapshots                     | 2 592 000         | **30 d**        |

Source of truth:
`internal/controlplane/server/timeseries_rollup.go:22-31` (`defaultRetentionSettings()`).

All seven fields are user-settable via the control-plane HTTP API:

- `GET  /api/settings/retention` — returns the current values (falling back to
  defaults if the operator has never written the row).
- `PUT  /api/settings/retention` — writes a complete `RetentionSettings`
  document. The server normalizes zero/negative values back to defaults to
  prevent accidental "retain nothing" configurations.

The retention worker (`timeseries_rollup.go`) reads the current settings on
every scheduled tick, so changes take effect without a restart.

### Default rationale

- **90 days audit (`AuditEventSeconds = 7 776 000`)** — long enough to cover
  typical quarterly security reviews and incident retrospectives, short
  enough that a default install does not silently accumulate PII. Operators
  with longer statutory obligations (SOX, HIPAA) must raise this.
- **30 days metrics (`MetricSnapshotSeconds = 2 592 000`)** — covers a full
  monthly operational review. Most metric snapshots are aggregate counters
  and do not contain PII; retention is driven by query cost, not by
  regulation.

---

## 2. Compliance matrix

The table below summarises the retention windows most Panvex deployments must
satisfy. Values in parentheses are the corresponding value in seconds — the
unit used by the `RetentionSettings` record and by `PUT /api/settings/retention`.

| Regime                | Scope                                    | Minimum retention     | Maximum retention                      | Panvex default recommendation        |
| --------------------- | ---------------------------------------- | --------------------- | -------------------------------------- | ------------------------------------ |
| **GDPR** (EU)         | Personal data, audit events tied to a DS | None (per purpose)    | "Strictly necessary" for the purpose   | 90 d (7 776 000); erase on DSR       |
| **UK GDPR / DPA 2018**| Same as EU GDPR                          | None (per purpose)    | "No longer than necessary"             | 90 d (7 776 000); erase on DSR       |
| **CCPA / CPRA** (US)  | Consumer personal information            | None                  | Must disclose retention period         | 90 d (7 776 000); erase on DSR       |
| **SOX** (US finance)  | Financial controls audit trail           | **7 years** (2520 d)  | No statutory cap                       | 2520 d (217 728 000)                 |
| **HIPAA** (US health) | Audit controls for PHI systems           | **6 years** (2160 d)  | No statutory cap                       | 2160 d (186 624 000)                 |
| **PCI-DSS v4**        | Cardholder data environment audit logs   | **1 year online** + 2 y cold storage (total ≥ 3 y) | No statutory cap | 365 d (31 536 000) online; export older data to archive |
| **ISO 27001**         | Information-security events              | 3 years (typical org policy) | Defined by ISMS policy         | 1095 d (94 608 000)                  |
| **Default / demo**    | Non-regulated workloads                  | 0                     | Operator's choice                      | **90 d** (current shipping default)  |

**Important caveats:**

- Panvex stores audit data **online** in PostgreSQL/SQLite. PCI-DSS
  specifically permits moving data older than one year to archival storage
  — Panvex does not yet offer an export-to-archive workflow, so PCI-DSS
  operators must either raise `AuditEventSeconds` to 3 years (94 608 000)
  or extract data externally (pg_dump, SQLite backup, scheduled S3 sync)
  before it is pruned.
- GDPR's "strictly necessary" is a purpose test, not a number. If the
  purpose is "security incident investigation", 90 days is defensible for
  most operators; if the purpose is "fraud prevention", a longer window may
  be justified.
- All of the above assume **security/audit** scope. Financial bookkeeping
  records, contracts, and similar business records are out of scope for
  Panvex — they live in the customer's own systems.

### Recommended values by regime (ready to paste)

```json
{
  "ts_raw_seconds":           86400,
  "ts_hourly_seconds":        2592000,
  "ts_dc_seconds":            86400,
  "ip_history_seconds":       2592000,
  "event_history_seconds":    86400,
  "audit_event_seconds":      7776000,
  "metric_snapshot_seconds":  2592000
}
```

The snippet above is the shipping default (GDPR/CCPA-friendly). Swap
`audit_event_seconds` to one of:

- `31536000`   — 1 year (PCI-DSS online minimum)
- `94608000`   — 3 years (PCI-DSS total, or ISO 27001 typical)
- `186624000`  — 6 years (HIPAA)
- `217728000`  — 7 years (SOX)

Apply with:

```bash
curl -X PUT https://panvex.example/api/settings/retention \
     -H 'Content-Type: application/json' \
     -b 'panvex_session=<cookie>' \
     --data @retention.json
```

---

## 3. Data-subject-request (DSR) workflow

Panvex supports the "right to erasure" / "right to be forgotten" family of
requests (GDPR art. 17, UK GDPR art. 17, CCPA § 1798.105, CPRA § 1798.106).
Until a dedicated CLI subcommand ships (see §4), erasures are a manual
operator procedure.

### 3.1 Data surfaces that may contain personal identifiers

| Table              | PII fields                                 | Erase?                                                      |
| ------------------ | ------------------------------------------ | ----------------------------------------------------------- |
| `audit_events`     | `actor_id`, `actor_username`, `request_ip`, `user_agent`, `payload_json` | **Yes** — redact on DSR |
| `metric_snapshots` | `payload_json` (may contain client IPs)    | Yes, if the operator has enabled IP-including snapshots     |
| `client_ip_history`| `ip_address`, `client_id`                  | Yes — this table is by definition PII                        |
| `sessions`         | `user_id`, `session_cookie_hash`           | Delete all active sessions for the subject                   |
| `control_room_events` | `actor_id`                              | Yes, if retained past the DSR cutoff                         |

Panvex does **not** store raw cardholder data, health information, or
financial transaction records, so HIPAA/PCI/SOX DSR-equivalents reduce to
the same surfaces above.

### 3.2 Standard operating procedure

1. **Intake.** Operator receives a DSR via the contact channel documented
   in the operator's privacy policy. Capture the subject's
   `user_id` or `actor_id` (from the admin UI's user page) and the request
   reference.
2. **Legal-hold check.** Verify that no active litigation-hold, regulatory
   subpoena, or open incident investigation requires retaining the
   subject's records. If a hold is active, respond per the hold's terms
   (GDPR art. 17(3)(e) provides an exemption) and stop here.
3. **Scope inventory.** Run the inventory query to confirm what records
   exist for the subject:

   ```sql
   -- Audit events the subject is the actor of.
   SELECT COUNT(*) FROM audit_events WHERE actor_id = :user_id;

   -- IP history rows referencing the subject's clients.
   SELECT COUNT(*) FROM client_ip_history
   WHERE client_id IN (SELECT id FROM clients WHERE owner_user_id = :user_id);

   -- Snapshots that embed the subject's id in payload.
   SELECT COUNT(*) FROM metric_snapshots
   WHERE payload_json::text LIKE '%' || :user_id || '%';
   ```

4. **Erase.** Open a transaction and execute:

   ```sql
   BEGIN;
   DELETE FROM audit_events     WHERE actor_id = :user_id;
   DELETE FROM client_ip_history WHERE client_id IN
     (SELECT id FROM clients WHERE owner_user_id = :user_id);
   DELETE FROM metric_snapshots WHERE payload_json::text LIKE '%' || :user_id || '%';
   DELETE FROM sessions         WHERE user_id = :user_id;
   -- Do NOT delete the users row itself unless the account is being closed;
   -- Panvex needs the row to enforce foreign-key integrity on historic
   -- rollout jobs. Instead, anonymise the user record:
   UPDATE users
      SET username      = 'erased-' || id,
          email         = NULL,
          display_name  = NULL,
          erased_at     = now()
    WHERE id = :user_id;
   COMMIT;
   ```

5. **Record the erasure.** Write a **new** audit event documenting the
   erasure itself (this event intentionally contains *no* PII about the
   subject — only the request reference and the operator who processed it):

   ```sql
   INSERT INTO audit_events (event_type, actor_id, payload_json, created_at)
   VALUES ('compliance.dsr_erasure',
           :operator_user_id,
           jsonb_build_object(
             'request_ref', :dsr_ref,
             'subject_erased', true,
             'tables_affected', jsonb_build_array(
               'audit_events','client_ip_history','metric_snapshots',
               'sessions','users'
             )
           ),
           now());
   ```

   The erasure record itself is retained under the normal
   `AuditEventSeconds` window — operators subject to SOX/HIPAA should raise
   that window so erasure records survive long enough to demonstrate the
   DSR was honoured.

6. **Respond to the subject.** Confirm erasure in writing within the
   regulatory deadline (GDPR: 30 days; CCPA: 45 days; extendable once).

### 3.3 Why not cascade-delete everything?

- Foreign-key integrity on rollout jobs, enrollment tokens, and client
  ownership requires the `users` row to remain. Anonymising the row
  preserves referential integrity while erasing the personal data.
- Audit-log erasure is intentionally **a single-table DELETE**, not a
  cascade, so operators can review the row count before committing.

---

## 4. CLI helper (deferred)

A future CLI subcommand will wrap the SQL above into a single operator
workflow:

```
cmd/control-plane dsr-erase --user-id <uuid> \
                             --request-ref <ticket-id> \
                             [--dry-run] \
                             [--confirm]
```

Planned behaviour:

1. Resolve `--user-id` to a user record; abort if the user does not exist.
2. Run the inventory query from §3.2 and print counts per table.
3. Warn if any table shows zero rows (likely wrong id) or if the user is
   protected by a legal hold annotation (not yet implemented —
   `users.legal_hold_until` column TBD).
4. With `--dry-run`, exit after printing. Without `--dry-run`, require
   `--confirm` or an interactive `yes/no` prompt before proceeding.
5. Run the transactional erase from §3.2.
6. Emit the `compliance.dsr_erasure` audit event.
7. Print a short receipt: rows affected per table, erasure event id, and
   the request reference.

**Implementation status:** deferred. The manual SOP in §3 is sufficient
for the (currently rare) DSR volume Panvex deployments see; the CLI
will be added when a deployment actually needs it, or bundled with the
next compliance pass. Tracking: **P2-COMPLIANCE-01 follow-up** (to be
filed against a future remediation plan).

---

## 5. Operator checklist

Before going into production with personal data:

- [ ] Decide the regulatory regime that applies (GDPR, SOX, HIPAA,
      PCI-DSS, ISO 27001, none).
- [ ] Set `audit_event_seconds` via `PUT /api/settings/retention` to match
      §2 for that regime.
- [ ] Document the retention choice and its legal basis in the operator's
      data-processing record (GDPR art. 30).
- [ ] Train at least two operators on the §3.2 SOP so DSRs can be honoured
      within the statutory deadline even if one operator is unavailable.
- [ ] If subject to PCI-DSS v4, establish an archive-export job for audit
      data older than 365 days.
- [ ] If subject to SOX or HIPAA, verify that database backups also honour
      the extended retention window — the `PUT /api/settings/retention`
      knob controls only the live tables.

---

## References

- [ADR-003: Retention — configurable TTL](../architecture/adr/003-retention-configurable-ttl.md)
- `internal/controlplane/storage/models.go` — `RetentionSettingsRecord`
- `internal/controlplane/storage/store.go` — `RetentionSettingsStore` interface
- `internal/controlplane/server/timeseries_rollup.go` — defaults and prune worker
- `internal/controlplane/server/http_retention.go` — `GET`/`PUT /api/settings/retention`
- Remediation plan v4 — P2-REL-03, P2-REL-04, P2-REL-05, P2-COMPLIANCE-01
