import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ClientsPageProps } from "@/shared/api/types-pages/clients";
import { ClientsPage } from "./ClientsPage";

// R-Q-13: ClientsPage smoke-test.

function makeProps(overrides: Partial<ClientsPageProps> = {}): ClientsPageProps {
  return {
    clients: [],
    viewMode: "list",
    autoThreshold: 24,
    ...overrides,
  };
}

describe("ClientsPage", () => {
  it("renders without throwing on empty client list", () => {
    expect(() => render(<ClientsPage {...makeProps()} />)).not.toThrow();
  });

  it("renders rows when clients are supplied", () => {
    const props = makeProps({
      clients: [
        {
          id: "c-1",
          name: "alpha",
          enabled: true,
          assignedNodesCount: 0,
          expirationRfc3339: "",
          trafficUsedBytes: 0,
          uniqueIpsUsed: 0,
          activeTcpConns: 0,
          dataQuotaBytes: 0,
          lastDeployStatus: "succeeded",
        },
      ],
    });
    const { container } = render(<ClientsPage {...props} />);
    expect(container.textContent).toContain("alpha");
  });
});
