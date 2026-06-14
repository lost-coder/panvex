// deltaClass / deltaArrow now live in the canonical ui/lib/format module
// (re-exported via @/ui). loadTone stays here — its thresholds are a
// load metric (high = bad), distinct from coverage semantics.
export function loadTone(value: number): { chart: string; text: string } {
  if (value >= 90) return { chart: "var(--color-status-error)", text: "text-status-error" };
  if (value >= 70) return { chart: "var(--color-status-warn)", text: "text-status-warn" };
  return { chart: "var(--color-accent)", text: "text-fg" };
}
