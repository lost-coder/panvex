import i18next from "i18next";

/**
 * BCP-47 locale for the active UI language. Date/time formatting must
 * follow the operator's chosen language (Profile → Language), not the
 * browser/OS locale — otherwise an English UI renders Russian dates and
 * native pickers (U-06). Defaults to en-US before i18n initialises.
 */
export function activeLocale(): string {
  return i18next.language?.startsWith("ru") ? "ru-RU" : "en-US";
}

/** Format a date/epoch/RFC3339 value as a locale-aware date + time. */
export function formatDateTime(input: number | string | Date): string {
  const d = input instanceof Date ? input : new Date(input);
  return d.toLocaleString(activeLocale());
}

/**
 * Format byte count to a human-readable string. Decimal (SI) base — this is
 * the single canonical byte formatter for the whole dashboard; do not
 * reimplement it per feature. Tiers: B < 1 KB ≤ KB < 1 MB ≤ MB < 1 GB ≤ GB.
 */
export function formatBytes(bytes: number): string {
  if (bytes <= 0) return "0 B";
  if (bytes > 1e9) return (bytes / 1e9).toFixed(1) + " GB";
  if (bytes > 1e6) return (bytes / 1e6).toFixed(1) + " MB";
  if (bytes > 1e3) return (bytes / 1e3).toFixed(1) + " KB";
  return bytes + " B";
}

/**
 * Trend-direction color class. up → ok (green), down → error (red),
 * flat/unknown → muted. Canonical: do not re-declare per feature.
 */
export function deltaClass(dir: "up" | "down" | "flat" | undefined): string {
  if (dir === "up") return "text-status-ok";
  if (dir === "down") return "text-status-error";
  return "text-fg-muted";
}

/** Trend-direction glyph matching deltaClass. */
export function deltaArrow(dir: "up" | "down" | "flat" | undefined): string {
  if (dir === "up") return "▲";
  if (dir === "down") return "▼";
  return "·";
}

/** Format seconds to "Xd Yh" uptime string */
export function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  return d > 0 ? `${d}d ${h}h` : `${h}h`;
}

/** Format unix epoch seconds to locale time string */
export function formatTime(epochSecs: number): string {
  return new Date(epochSecs * 1000).toLocaleTimeString(activeLocale());
}

/** Format byte quota: 0 = "Unlimited", otherwise human-readable */
export function formatQuota(bytes: number): string {
  if (bytes === 0) return "Unlimited";
  return formatBytes(bytes);
}

/** Format RFC3339 expiry: empty = "Never", otherwise locale date */
export function formatExpiry(rfc3339: string): string {
  if (!rfc3339) return "Never";
  return new Date(rfc3339).toLocaleDateString(activeLocale());
}

/** Format unix timestamp as relative age ("just now", "5m ago", "2d ago") */
export function formatAge(unixSecs: number): string {
  const diff = Math.floor(Date.now() / 1000 - unixSecs);
  if (diff < 60) return "just now";
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

/** Convert seconds to a display-friendly value + unit pair */
export function secondsToDisplay(seconds: number): { value: number; unit: string } {
  if (seconds >= 86400 && seconds % 86400 === 0) return { value: seconds / 86400, unit: "days" };
  if (seconds >= 3600 && seconds % 3600 === 0) return { value: seconds / 3600, unit: "hours" };
  if (seconds >= 60 && seconds % 60 === 0) return { value: seconds / 60, unit: "minutes" };
  return { value: seconds, unit: "seconds" };
}

/**
 * Short-form identifier. Returns the first `length` characters followed by
 * an ellipsis when the id is longer than `length + 2`; otherwise returns the
 * id unchanged. Empty/nullish ids collapse to the "—" placeholder.
 */
export function shortId(id: string | undefined | null, length = 8): string {
  if (!id) return "—";
  return id.length > length + 2 ? `${id.slice(0, length)}…` : id;
}

/** Convert display value + unit back to seconds */
export function displayToSeconds(value: number, unit: string): number {
  switch (unit) {
    case "days":
      return value * 86400;
    case "hours":
      return value * 3600;
    case "minutes":
      return value * 60;
    default:
      return value;
  }
}
