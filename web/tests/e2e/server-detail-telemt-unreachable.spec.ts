import { expect, test } from "@playwright/test";

import { mockTelemtUnreachableAgent } from "./fixtures/agents";
import { mockApi } from "./helpers/mock-api";

/**
 * Smoke: server-detail page when telemt_reachable=false renders the
 * TelemtUnreachableBanner and suppresses the mode-specific layout
 * (DirectRelayDesktop / DirectRelayMobile / MeDownHero).
 *
 * The fixture wires a direct-mode agent with telemt_reachable=false so
 * the page boots without 404s (mockApi covers auth/version/fleet) and
 * the single detail call returns our unreachable fixture.
 */
test("server detail shows banner and hides mode when telemt is unreachable", async ({ page }) => {
  await mockApi(page);

  await page.route(/\/api\/telemetry\/servers\/[^/]+$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(mockTelemtUnreachableAgent()),
    }),
  );

  // Stub history endpoints so their schema-validated queries resolve cleanly
  // rather than throwing ApiSchemaError and polluting the error boundary.
  await page.route(/\/api\/telemetry\/servers\/[^/]+\/history\//, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ points: [], resolution: "raw" }),
    }),
  );

  await page.goto("/servers/agent-telemt-down-1");

  // Banner is visible — the <div role="alert"> in TelemtUnreachableBanner.
  await expect(page.getByRole("alert")).toContainText(/Связь с Telemt потеряна/i);

  // Mode badge is shown in the desktop ServerHero when telemtReachable=false.
  // The hero is md:block so in a desktop-width viewport it is visible.
  await expect(page.getByText("Режим неизвестен")).toBeVisible();

  // The direct-relay layout (upstream-health heading) must NOT be rendered
  // because the entire mode section is replaced by the banner.
  await expect(
    page.getByRole("heading", { name: /Upstream health/i }),
  ).toHaveCount(0);
});
