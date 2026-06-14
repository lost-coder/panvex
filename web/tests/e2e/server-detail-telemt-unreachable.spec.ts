import { expect, test } from "@playwright/test";

import { mockTelemtUnreachableAgent } from "./fixtures/agents";
import { mockApi } from "./helpers/mock-api";

/**
 * Smoke: server-detail page when telemt_reachable=false renders the
 * TelemtUnreachableBanner and suppresses the mode-specific layout
 * (DirectRelayDesktop / DirectRelayMobile / MeDownHero).
 *
 * The fixture wires a direct-mode agent with telemt_unreachable=true so
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

  // detail-boost + refresh-diagnostics fire on mount; without explicit
  // stubs they fall through to mockApi's /api/telemetry/servers PREFIX
  // route, get the list fixture back, fail schema validation, and trip
  // the error boundary (role="alert") — which is what broke this spec
  // in CI on 2026-05-13.
  await page.route(/\/api\/telemetry\/servers\/[^/]+\/detail-boost$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ active: false, expires_at_unix: 0, remaining_seconds: 0 }),
    }),
  );
  await page.route(/\/api\/telemetry\/servers\/[^/]+\/refresh-diagnostics$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ job_id: "stub-job-1", status: "queued" }),
    }),
  );

  await page.goto("/servers/agent-telemt-down-1");

  // Banner is visible — the <div role="alert"> in TelemtUnreachableBanner.
  // The app runs in English (default locale) in e2e tests; use the EN
  // translation key value (detail.telemtLost = "Telemt connection lost").
  await expect(page.getByRole("alert")).toContainText(/Telemt connection lost/i);

  // Mode badge is shown in the desktop ServerHero when telemtReachable=false.
  // EN key: detail.modeUnknown = "Mode unknown". The hero is md:block so in
  // a desktop-width viewport it is visible.
  await expect(page.getByText("Mode unknown")).toBeVisible();

  // The direct-relay layout (upstream-health heading) must NOT be rendered
  // because the entire mode section is replaced by the banner.
  await expect(
    page.getByRole("heading", { name: /Upstream health/i }),
  ).toHaveCount(0);
});
