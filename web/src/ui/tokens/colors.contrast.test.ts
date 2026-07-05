import { describe, expect, it } from "vitest";
import { bgColors, fgColors, statusColors } from "./colors";

// NOTE(P5): this WCAG guard mirrors the palette a THIRD time (the
// light-theme literals below + the imported dark-theme statusColors). The
// canonical sources are the --panvex-* CSS vars in src/ui-kit.css and their
// JS mirror in src/ui/tokens/colors.ts — keep all three in sync when the
// palette changes.
//
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

describe("StatusPill warn ink contrast (both themes, WCAG AA)", () => {
  // Fills + inks are CSS-only (themed --panvex-status-warn / -ink). Mirror the
  // literals here so the pill's text-vs-fill pairing is guarded, not just the
  // page/muted token pairs.
  const warnFillDark = "#f59e0b";
  const warnInkDark = "#1a1306";
  const warnFillLight = "#b45309";
  const warnInkLight = "#ffffff";
  it("dark amber fill vs dark ink clears AA (4.5:1)", () => {
    expect(contrast(warnInkDark, warnFillDark)).toBeGreaterThanOrEqual(4.5);
  });
  it("light amber fill vs white ink clears AA (4.5:1)", () => {
    expect(contrast(warnInkLight, warnFillLight)).toBeGreaterThanOrEqual(4.5);
  });
});

describe("StatusPill error ink contrast (both themes, WCAG AA)", () => {
  // Solid error pill: white text on the deepened error fill (themed
  // --panvex-status-error-strong). Mirror the literals so the pill's
  // text-vs-fill pairing is guarded.
  const white = "#ffffff";
  const errorStrongDark = "#c81e1e";
  const errorStrongLight = "#dc2626";
  it("dark error-strong fill vs white clears AA (4.5:1)", () => {
    expect(contrast(white, errorStrongDark)).toBeGreaterThanOrEqual(4.5);
  });
  it("light error-strong fill vs white clears AA (4.5:1)", () => {
    expect(contrast(white, errorStrongLight)).toBeGreaterThanOrEqual(4.5);
  });
});

describe("light theme token contrast (WCAG AA, text on light surfaces)", () => {
  // Light tokens are CSS-only (--panvex-* under .light); mirror the literals
  // so light-theme text legibility is guarded alongside dark.
  const bg = "#f5f6f8";
  const card = "#ffffff";
  const pairs: Array<[string, string]> = [
    ["#5b6470", bg], ["#5b6470", card],   // fg-muted
    ["#047857", bg], ["#047857", card],   // status-ok (deepened)
    ["#b45309", bg],                       // status-warn
    ["#c81e1e", bg], ["#c81e1e", card],   // status-error (deepened)
    ["#2563eb", bg],                       // accent
  ];
  it.each(pairs)("%s on %s clears AA (4.5:1)", (fg, surface) => {
    expect(contrast(fg, surface)).toBeGreaterThanOrEqual(4.5);
  });
});

describe("dark status tokens as small text (WCAG AA)", () => {
  // statusColors are the dark-theme values, used as `text-status-*` foreground
  // on the page bg (e.g. ClientExpiryCell / load cells). Guard them too.
  it.each([
    ["ok", statusColors.ok],
    ["warn", statusColors.warn],
    ["error", statusColors.error],
  ])("status-%s text clears AA on page bg (4.5:1)", (_name, hex) => {
    expect(contrast(hex, bgColors.DEFAULT)).toBeGreaterThanOrEqual(4.5);
  });
});

describe("light status-warn on card (WCAG AA)", () => {
  it("light status-warn #b45309 clears AA on card #fff", () => {
    expect(contrast("#b45309", "#ffffff")).toBeGreaterThanOrEqual(4.5);
  });
});
