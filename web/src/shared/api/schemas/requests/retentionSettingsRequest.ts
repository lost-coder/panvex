import { z } from "zod";

/**
 * Mirrors the subset of `RetentionSettings` (internal/controlplane/server/
 * timeseries_rollup.go) that the operator UI actually edits and sends via
 * PUT /settings/retention — see SettingsSections.tsx's retention field
 * list. The Go struct carries additional fields (audit_event_seconds,
 * metric_snapshot_seconds, jobs_seconds, webhook_outbox_seconds,
 * enrollment_token_seconds) that are not operator-editable from this form;
 * `normalizeRetentionSettings` fills those in server-side when they arrive
 * as zero, so we don't need to round-trip them here.
 */
export const retentionSettingsRequestSchema = z.object({
  ts_raw_seconds: z.number().int(),
  ts_hourly_seconds: z.number().int(),
  ts_dc_seconds: z.number().int(),
  ip_history_seconds: z.number().int(),
  event_history_seconds: z.number().int(),
});

export type RetentionSettingsRequest = z.infer<typeof retentionSettingsRequestSchema>;
