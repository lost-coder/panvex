import { describe, expect, it } from "vitest";
import { bgColors, fgColors } from "./colors";

// WCAG 2.1 relative luminance + contrast ratio (sRGB hex → ratio).
function luminance(hex: string): number {
  const v = hex.replace("#", "");
  const ch = [0, 2, 4].map((i) => parseInt(v.slice(i, i + 2), 16) / 255);
  const lin = ch.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * (lin[0] ?? 0) + 0.7152 * (lin[1] ?? 0) + 0.0722 * (lin[2] ?? 0);
}
function contrast(a: string, b: string): number {
  const sorted = [luminance(a), luminance(b)].sort((x, y) => y - x);
  const l1 = sorted[0] ?? 0;
  const l2 = sorted[1] ?? 0;
  return (l1 + 0.05) / (l2 + 0.05);
}

describe("token contrast (dark theme, WCAG AA)", () => {
  it("muted text on page bg clears AA for normal text (4.5:1)", () => {
    expect(contrast(fgColors.muted, bgColors.DEFAULT)).toBeGreaterThanOrEqual(4.5);
  });
  it("muted text on card bg clears AA for normal text (4.5:1)", () => {
    expect(contrast(fgColors.muted, bgColors.card)).toBeGreaterThanOrEqual(4.5);
  });
  it("default text on page bg clears AAA (7:1)", () => {
    expect(contrast(fgColors.DEFAULT, bgColors.DEFAULT)).toBeGreaterThanOrEqual(7);
  });
});
