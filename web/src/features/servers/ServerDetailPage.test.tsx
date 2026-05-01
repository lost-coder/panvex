import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ServerDetailPageProps } from "@/shared/api/types-pages/server-detail";
import { ServerDetailPage } from "./ServerDetailPage";

// R-Q-13: ServerDetailPage smoke-test. The fixture is large because the
// page surfaces every /v1/* endpoint at once — keeping it inline avoids
// a separate test-fixtures module that would drift faster than the page
// itself.

function makeProps(): ServerDetailPageProps {
  return {
    server: {
      id: "n-1",
      name: "node-a",
      status: "ok",
      systemInfo: {
        version: "1.2.3",
        targetArch: "x86_64",
        targetOs: "linux",
        buildProfile: "release",
        uptimeSeconds: 1234,
        configHash: "abc",
        configPath: "/etc/telemt.toml",
        configReloadCount: 0,
      },
      gates: {
        acceptingNewConnections: true,
        meRuntimeReady: true,
        useMiddleProxy: true,
        me2dcFallbackEnabled: false,
        rerouteActive: false,
        startupStatus: "ready",
        startupProgressPct: 100,
        degraded: false,
        readOnly: false,
      },
      dcs: [],
      connections: {
        current: 0,
        currentMe: 0,
        currentDirect: 0,
        activeUsers: 0,
        staleCacheUsed: false,
        topByConnections: [],
        topByThroughput: [],
      },
      summary: {
        connectionsTotal: 0,
        connectionsBadTotal: 0,
        handshakeTimeoutsTotal: 0,
        configuredUsers: 0,
      },
      upstreams: [],
      events: [],
      eventsDroppedTotal: 0,
      useMiddleProxy: true,
      meRuntimeReady: true,
      me2dcFallbackEnabled: false,
      transportMode: "middle_proxy",
      fallbackEnteredAtUnix: null,
    },
  };
}

describe("ServerDetailPage", () => {
  it("renders the server name", () => {
    const { container } = render(<ServerDetailPage {...makeProps()} />);
    expect(container.textContent).toContain("node-a");
  });

  it("renders without throwing on a minimal but complete fixture", () => {
    expect(() => render(<ServerDetailPage {...makeProps()} />)).not.toThrow();
  });
});
