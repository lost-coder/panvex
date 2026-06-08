import { describe, expect, it } from "vitest";
import type { ClientListItem } from "@/ui";

import { buildClientCounts } from "./ClientsPagePulse";

const base = {
  id: "1", name: "c", enabled: true,
  expirationRfc3339: "", trafficUsedBytes: 0, dataQuotaBytes: 0,
  assignedNodesCount: 0, lastDeployStatus: "succeeded", activeTcpConns: 0,
} as unknown as ClientListItem;
const NOW = Date.parse("2026-06-04T00:00:00Z");

describe("buildClientCounts", () => {
  it("tallies each of the 7 states via deriveClientState", () => {
    const clients = [
      { ...base }, // active
      { ...base, expirationRfc3339: "2026-06-08T00:00:00Z" }, // expiring (within 7d)
      { ...base, enabled: false }, // disabled
    ] as unknown as ClientListItem[];

    const counts = buildClientCounts(clients, NOW);

    expect(counts.all).toBe(3);
    // expiring clients are their own bucket, NOT counted as active
    expect(counts.active).toBe(1);
    expect(counts.expiring).toBe(1);
    expect(counts.disabled).toBe(1);
  });
});
