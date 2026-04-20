/** Format byte count to human-readable string */
export function formatBytes(bytes: number): string {
  if (bytes > 1e9) return (bytes / 1e9).toFixed(1) + " GB";
  if (bytes > 1e6) return (bytes / 1e6).toFixed(1) + " MB";
  return bytes + " B";
}

/** Format seconds to "Xd Yh" uptime string */
export function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  return d > 0 ? `${d}d ${h}h` : `${h}h`;
}

/** Format unix epoch seconds to locale time string */
export function formatTime(epochSecs: number): string {
  return new Date(epochSecs * 1000).toLocaleTimeString();
}

/** Format byte quota: 0 = "Unlimited", otherwise human-readable */
export function formatQuota(bytes: number): string {
  if (bytes === 0) return "Unlimited";
  return formatBytes(bytes);
}

/** Format RFC3339 expiry: empty = "Never", otherwise locale date */
export function formatExpiry(rfc3339: string): string {
  if (!rfc3339) return "Never";
  return new Date(rfc3339).toLocaleDateString();
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
