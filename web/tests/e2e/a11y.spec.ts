import { AxeBuilder } from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { mockDirectAgent } from "./fixtures/agents";
import { mockApi } from "./helpers/mock-api";

/**
 * Accessibility smoke: run axe on every reachable top-level route. A
 * WCAG 2.1 AA violation on a primary page fails CI before it hits
 * production.
 *
 * U2: colour-contrast is now ENABLED globally. It was previously
 * disabled because Panvex's design tokens tripped axe on backdrop-blur
 * gradients that are *not* readable surfaces. Rather than blanket-
 * disable the rule, real violations are fixed at the token/class level
 * and any unavoidable false positive is excluded per-node (axe
 * `exclude()`) with a TODO — never globally.
 */

const SERVER_DETAIL_ID = "agent-direct-1";

const PAGES = [
  { name: "login", path: "/login" },
  { name: "dashboard", path: "/" },
  { name: "servers", path: "/servers" },
  // server-detail needs a per-id /api/telemetry/servers/{id} stub on top
  // of mockApi; wired in the test body below with the direct-mode fixture.
  { name: "server-detail", path: `/servers/${SERVER_DETAIL_ID}` },
  { name: "add-server", path: "/servers/add" },
  { name: "clients", path: "/clients" },
  { name: "settings", path: "/settings" },
  { name: "settings-users", path: "/settings/users" },
  { name: "profile", path: "/profile" },
  { name: "activity", path: "/activity" },
  { name: "enrollment-attempts", path: "/enrollment-attempts" },
  { name: "fleet-groups", path: "/fleet-groups" },
];

/*
 * Routes intentionally NOT covered here, and why:
 *  - /clients/$clientId  — client-detail needs a per-client fixture
 *    (secret, limits, deployment rollout state) that mockApi does not
 *    yet stub; add once a client fixture exists.
 *  - /clients/discovered — depends on a discovered-clients fixture.
 *  - /fleet-groups/$fleetGroupId — needs a fleet-group detail fixture.
 *  - /servers/enrollment — multi-step wizard with live gRPC attempt
 *    polling; not reachable hermetically without an enrollment fixture.
 */

test.describe("Accessibility smoke", () => {
  for (const p of PAGES) {
    test(`${p.name} — no axe violations`, async ({ page }) => {
      // Server-detail pulls per-agent telemetry that the generic mockApi
      // does not stub; fulfil it with the direct-mode fixture so the page
      // renders real content (not the error boundary) for the audit.
      if (p.name === "server-detail") {
        const agent = mockDirectAgent({
          agentId: SERVER_DETAIL_ID,
          nodeName: "node-direct",
        });
        await page.route(/\/api\/telemetry\/servers\/[^/]+$/, (route) =>
          route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify(agent),
          }),
        );
      }

      await mockApi(page);
      await page.goto(p.path);

      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        // TODO(a11y): if recharts SVG tick labels surface as color-contrast
        // false positives once charts render, scope a per-node exclude here
        // (e.g. `.exclude(".recharts-cartesian-axis-tick")`) with a tracking
        // issue — do NOT re-add a global disableRules(["color-contrast"]).
        .analyze();

      expect(
        results.violations,
        JSON.stringify(results.violations, null, 2),
      ).toEqual([]);
    });
  }
});
