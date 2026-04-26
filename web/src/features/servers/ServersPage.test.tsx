import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ServersPageProps } from "@/shared/api/types-pages/servers";
import { ServersPage } from "./ServersPage";

// R-Q-13: ServersPage smoke-test.

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
      servers: [
        {
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
        },
      ],
    });
    const { container } = render(<ServersPage {...props} />);
    expect(container.textContent).toContain("node-a");
  });
});
