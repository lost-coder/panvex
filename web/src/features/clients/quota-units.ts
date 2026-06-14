// Byte ⇄ human-unit conversion for the client data quota. Binary (1024)
// multipliers match how Telemt accounts traffic. The form state keeps raw
// bytes (the API contract is unchanged); these helpers only translate at
// the editing surface.
export type QuotaUnit = "MB" | "GB" | "TB";

export const QUOTA_UNITS: readonly QuotaUnit[] = ["MB", "GB", "TB"];

export const UNIT_FACTOR: Record<QuotaUnit, number> = {
  MB: 1024 ** 2,
  GB: 1024 ** 3,
  TB: 1024 ** 4,
};

const round2 = (n: number) => Math.round(n * 100) / 100;

/**
 * Display `bytes` in the largest unit the value reaches, rounded to two
 * decimals. 0 / negative / NaN render as 0 GB ("unlimited"); a positive
 * quota never collapses to a displayed 0 (floored at 0.01 MB).
 */
export function quotaToDisplay(bytes: number): { value: number; unit: QuotaUnit } {
  if (!Number.isFinite(bytes) || bytes <= 0) return { value: 0, unit: "GB" };
  for (const unit of ["TB", "GB", "MB"] as const) {
    if (bytes >= UNIT_FACTOR[unit]) {
      return { value: round2(bytes / UNIT_FACTOR[unit]), unit };
    }
  }
  return { value: Math.max(0.01, round2(bytes / UNIT_FACTOR.MB)), unit: "MB" };
}

/**
 * Convert a human-entered `value` + `unit` back to bytes. Non-positive or
 * non-finite inputs return 0 (= unlimited).
 */
export function displayToQuota(value: number, unit: QuotaUnit): number {
  if (!Number.isFinite(value) || value <= 0) return 0;
  return Math.round(value * UNIT_FACTOR[unit]);
}
