import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { DashboardNodeData, DashboardOverviewData } from "@/ui";
import { FleetPanel } from "./FleetPanel";

// Render test for FleetRow's headline behaviour: a state-driven status
// pill, a localized reason line, and a severity-based left-border tint.
// i18n is bootstrapped globally in vitest.setup.ts (default language
// "en", real resources loaded), so reason/label strings resolve to the
// English translations rather than raw keys.

function makeNode(overrides: Partial<DashboardNodeData> & Pick<DashboardNodeData, "id" | "name" | "state">): DashboardNodeData {
  return {
    status: "ok",
    reason: "",
    connections: 0,
    trafficBytes: 0,
    cpuPct: 0,
    memPct: 0,
    dcs: [],
    ...overrides,
  };
}

function makeOverview(): DashboardOverviewData {
  return {
    kpis: [],
    trends: [],
    alerts: [],
    attentionNodes: [
      makeNode({
        id: "n-down",
        name: "edge-down",
        status: "error",
        state: "down",
        reason: "all upstreams down",
      }),
      makeNode({
        id: "n-pending",
        name: "edge-pending",
        state: "pending",
        reason: "Startup is still in progress",
      }),
    ],
    healthyNodes: [makeNode({ id: "n-ok", name: "edge-ok", state: "ok", reason: "" })],
  };
}

function rowFor(name: string): HTMLElement {
  const row = screen.getByText(name).closest("button");
  expect(row).not.toBeNull();
  return row as HTMLElement;
}

describe("FleetPanel / FleetRow", () => {
  it("renders the localized reason line and severity tint for a down node", () => {
    render(<FleetPanel data={makeOverview()} />);

    // 1. Localized reason line: "all upstreams down" → reason.allUpstreamsDown.
    expect(screen.getByText("All upstreams down")).toBeInTheDocument();

    // 2. Down row carries the alarm-red left-border tint.
    expect(rowFor("edge-down").className).toContain("border-l-status-error");
  });

  it("keeps pending and healthy rows calm (no error/warn tint)", () => {
    render(<FleetPanel data={makeOverview()} />);

    // 3a. Pending row is neutral — no error/warn tint, transparent border.
    const pendingRow = rowFor("edge-pending");
    expect(pendingRow.className).not.toContain("border-l-status-error");
    expect(pendingRow.className).not.toContain("border-l-status-warn");
    expect(pendingRow.className).toContain("border-l-transparent");

    // 3b. Healthy row renders, stays neutral, and shows no reason line.
    const okRow = rowFor("edge-ok");
    expect(okRow.className).toContain("border-l-transparent");
    expect(okRow.className).not.toContain("border-l-status-error");
  });
});
