import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ServerListItem } from "@/ui";
import { ServerListView } from "./ServerListView";

// Render test for ServerListView's status pill + localized reason line.
// ServerListView mounts BOTH a desktop DataTable (`hidden md:block`) and a
// mobile NodeCard list (`md:hidden`); jsdom mounts both, so any text shared
// by both views appears twice — assert with getAllByText(...).length.
//
// i18n is bootstrapped globally in vitest.setup.ts (default language "en",
// real resources), so status labels and reason strings resolve to English:
//   common:status.down            → "DOWN"
//   common:reason.allUpstreamsDown → "All upstreams down"

function makeServer(
  overrides: Partial<ServerListItem> & Pick<ServerListItem, "id" | "name">,
): ServerListItem {
  return {
    status: "ok",
    state: "ok",
    reason: "",
    connections: 0,
    trafficBytes: 0,
    cpuPct: 0,
    memPct: 0,
    dcCoveragePct: 0,
    uptimeSeconds: 0,
    fleetGroupId: "",
    useMiddleProxy: false,
    meRuntimeReady: false,
    me2dcFallbackEnabled: false,
    healthyUpstreams: 0,
    totalUpstreams: 0,
    healthyDcs: 0,
    totalDcs: 0,
    severity: "ok",
    telemtUnreachable: false,
    telemtUnreachableSinceUnix: 0,
    ...overrides,
  };
}

describe("ServerListView / status pill + reason", () => {
  it("renders the status pill and localized reason, falling back to IP when healthy", () => {
    render(
      <ServerListView
        servers={[
          makeServer({ id: "1", name: "edge-down", state: "down", reason: "all upstreams down" }),
          makeServer({ id: "2", name: "edge-ok", state: "ok", reason: "", ip: "10.0.0.1" }),
        ]}
        visibleColumns={{ transport: true, users: true, traffic: true, uptime: true, load: true }}
      />,
    );

    // 1. Down server's localized reason line.
    expect(screen.getAllByText("All upstreams down").length).toBeGreaterThan(0);

    // 2. Down server's status pill label.
    expect(screen.getAllByText("DOWN").length).toBeGreaterThan(0);

    // 3. Healthy server falls back to its IP (it has no reason line).
    expect(screen.getAllByText("10.0.0.1").length).toBeGreaterThan(0);

    // 4. Both server names render (in both desktop + mobile views).
    expect(screen.getAllByText("edge-down").length).toBeGreaterThan(0);
    expect(screen.getAllByText("edge-ok").length).toBeGreaterThan(0);
  });
});
