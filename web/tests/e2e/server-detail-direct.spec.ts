import { expect, test } from "@playwright/test";

import { mockDirectAgent } from "./fixtures/agents";
import { mockApi } from "./helpers/mock-api";

/**
 * Smoke: server-detail page in direct mode renders the direct-relay
 * layout (Phase 10.1 of the direct-mode panel rollout).
 *
 * The plan-supplied URL pattern was `**\/api/agents/**`, but the actual
 * server-detail endpoint is `/api/telemetry/servers/{agentID}`. The
 * route below pins exactly that path so the fixture replaces only the
 * detail call — `mockApi` covers the surrounding auth/version/fleet
 * endpoints so the page boots without 404s.
 */
test("server detail in direct mode renders direct-relay layout", async ({ page }) => {
  await mockApi(page);

  await page.route(/\/api\/telemetry\/servers\/[^/]+$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(mockDirectAgent()),
    }),
  );

  await page.goto("/servers/agent-direct-1");

  // Both DirectRelayMobile and DirectRelayDesktop render simultaneously
  // — only one wrapper (md:hidden / hidden md:block) is visible at a
  // given viewport. Filter to visible matches so the spec stays
  // viewport-agnostic and matches the intent of "the direct-relay
  // layout is rendered".
  const upstreamHealth = page
    .getByRole("heading", { name: /Upstream health/i })
    .locator("visible=true");
  await expect(upstreamHealth).toHaveCount(1);
  await expect(page.getByText(/Data Centers/i)).not.toBeVisible();
  await expect(page.getByText(/no reachable DCs/i)).not.toBeVisible();
});
