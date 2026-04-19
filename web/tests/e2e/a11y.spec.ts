import { AxeBuilder } from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { mockApi } from "./helpers/mock-api";

/**
 * Accessibility smoke: run axe on every top-level route. A WCAG 2.1 AA
 * violation on a primary page fails CI before it hits production.
 *
 * Colour-contrast is intentionally excluded from the initial pass —
 * Panvex uses custom design tokens that axe flags on backdrop blur
 * gradients which are *not* readable surfaces. Re-enable once we have
 * a token-level contrast audit (tracked under 6.11).
 */
const PAGES = [
  { name: "dashboard", path: "/" },
  { name: "servers", path: "/servers" },
  { name: "clients", path: "/clients" },
  { name: "settings", path: "/settings" },
];

test.describe("Accessibility smoke", () => {
  for (const p of PAGES) {
    test(`${p.name} — no axe violations (excluding color-contrast)`, async ({ page }) => {
      await mockApi(page);
      await page.goto(p.path);

      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .disableRules(["color-contrast"])
        .analyze();

      expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
    });
  }
});
