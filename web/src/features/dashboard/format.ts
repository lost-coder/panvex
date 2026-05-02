export function deltaClass(dir: "up" | "down" | "flat" | undefined): string {
  if (dir === "up") return "text-status-ok";
  if (dir === "down") return "text-status-error";
  return "text-fg-muted";
}

export function deltaArrow(dir: "up" | "down" | "flat" | undefined): string {
  if (dir === "up") return "▲";
  if (dir === "down") return "▼";
  return "·";
}

export function loadTone(value: number): { chart: string; text: string } {
  if (value >= 90) return { chart: "var(--color-status-error)", text: "text-status-error" };
  if (value >= 70) return { chart: "var(--color-status-warn)", text: "text-status-warn" };
  return { chart: "var(--color-accent)", text: "text-fg" };
}
