export const statusColors = {
  ok: "#34d399",
  warn: "#f59e0b",
  error: "#ef4444",
} as const;

export const bgColors = {
  DEFAULT: "#0b0d12",
  card: "#141820",
  cardHi: "#1a1f2a",
  hover: "#1e2430",
} as const;

export const fgColors = {
  DEFAULT: "#f3f5f9",
  muted: "#9aa3b2",
  faint: "#2a3040",
} as const;

export const accentColor = "#60a5fa";

export type Status = "ok" | "warn" | "error";
