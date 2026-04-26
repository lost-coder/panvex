import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { DiscoveredClientsPageProps } from "@/shared/api/types-pages/discovered-clients";
import { DiscoveredClientsPage } from "./DiscoveredClientsPage";

// R-Q-13: DiscoveredClientsPage smoke-test.

function makeProps(overrides: Partial<DiscoveredClientsPageProps> = {}): DiscoveredClientsPageProps {
  return {
    clients: [],
    ...overrides,
  };
}

describe("DiscoveredClientsPage", () => {
  it("renders without throwing on empty list", () => {
    expect(() => render(<DiscoveredClientsPage {...makeProps()} />)).not.toThrow();
  });

  it("renders without throwing when at least one discovered client is present", () => {
    const now = Math.floor(Date.now() / 1000);
    const props = makeProps({
      clients: [
        {
          id: "d-1",
          agentId: "a-1",
          nodeName: "node-a",
          clientName: "discovered-name",
          status: "pending_review",
          totalOctets: 0,
          currentConnections: 0,
          activeUniqueIps: 0,
          links: { classic: [], secure: [], tls: [] },
          maxTcpConns: 0,
          maxUniqueIps: 0,
          dataQuotaBytes: 0,
          expiration: "",
          discoveredAtUnix: now,
          updatedAtUnix: now,
        },
      ],
    });
    expect(() => render(<DiscoveredClientsPage {...props} />)).not.toThrow();
  });
});
