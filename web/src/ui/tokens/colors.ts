// NOTE(P5): the hex values here MUST match the dark-theme --panvex-* CSS
// variables in src/ui-kit.css (~L42-50) — this is the JS mirror of the
// palette for SVG/inline fills that have no access to CSS vars. Change the
// palette in THREE places: ui-kit.css (both themes), this file, and the
// light-theme literals in colors.contrast.test.ts (the WCAG guard).
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

/** Pill tones: the three severities plus a calm neutral (PENDING/DISABLED). */
export type PillTone = Status | "neutral";
