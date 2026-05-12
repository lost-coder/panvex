import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ServerListItem, ServersPageProps } from "@/shared/api/types-pages/servers";
import { ServersPage } from "./ServersPage";

// R-Q-13: ServersPage smoke-test.

function mockServer(overrides: Partial<ServerListItem> = {}): ServerListItem {
  return {
    id: "n-1",
    name: "node-a",
    status: "ok",
    connections: 10,
    trafficBytes: 0,
    cpuPct: 1,
    memPct: 1,
    dcCoveragePct: 100,
    uptimeSeconds: 60,
    fleetGroupId: "fg-1",
    useMiddleProxy: true,
    meRuntimeReady: true,
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

function makeProps(overrides: Partial<ServersPageProps> = {}): ServersPageProps {
  return {
    servers: [],
    ...overrides,
  };
}

describe("ServersPage", () => {
  it("renders without throwing on empty list", () => {
    expect(() => render(<ServersPage {...makeProps()} />)).not.toThrow();
  });

  it("renders rows when servers are supplied", () => {
    const props = makeProps({
      servers: [mockServer()],
    });
    const { container } = render(<ServersPage {...props} />);
    expect(container.textContent).toContain("node-a");
  });

  it("renders a Direct transport badge for direct-mode servers", () => {
    const directServer = mockServer({
      id: "n-direct",
      name: "node-direct",
      useMiddleProxy: false,
      meRuntimeReady: false,
      me2dcFallbackEnabled: false,
      healthyUpstreams: 3,
      totalUpstreams: 3,
      severity: "ok",
    });
    render(<ServersPage {...makeProps({ servers: [directServer] })} />);
    // Mobile (NodeCard) and desktop (DataTable) layouts both render the
    // badge — jsdom evaluates both branches regardless of CSS media
    // queries, so we assert at least one match instead of expecting
    // exactly one element.
    expect(screen.getAllByText(/Direct 3\/3/i).length).toBeGreaterThan(0);
  });
});
