import { describe, expect, it } from "vitest";
import { deriveClientState } from "./ClientsPageCells";
import type { ClientListItem } from "@/ui";

const base = {
  id: "1", name: "c", enabled: true,
  expirationRfc3339: "", trafficUsedBytes: 0, dataQuotaBytes: 0,
  assignedNodesCount: 0, lastDeployStatus: "succeeded",
} as unknown as ClientListItem;
const NOW = Date.parse("2026-06-04T00:00:00Z");

describe("deriveClientState", () => {
  it("expired wins over everything", () => {
    expect(deriveClientState({ ...base, enabled: false, expirationRfc3339: "2020-01-01T00:00:00Z" }, NOW)).toBe("expired");
  });
  it("disabled when not enabled", () => {
    expect(deriveClientState({ ...base, enabled: false }, NOW)).toBe("disabled");
  });
  it("deploy_failed when lastDeployStatus is failed", () => {
    expect(deriveClientState({ ...base, lastDeployStatus: "failed", assignedNodesCount: 1 }, NOW)).toBe("deploy_failed");
  });
  it("over_quota when used >= quota*nodes", () => {
    expect(deriveClientState({ ...base, dataQuotaBytes: 100, assignedNodesCount: 2, trafficUsedBytes: 200 }, NOW)).toBe("over_quota");
  });
  it("not_deployed when assigned but not succeeded (idle/pending)", () => {
    expect(deriveClientState({ ...base, assignedNodesCount: 1, lastDeployStatus: "pending" }, NOW)).toBe("not_deployed");
  });
  it("expiring within 7 days", () => {
    expect(deriveClientState({ ...base, expirationRfc3339: "2026-06-08T00:00:00Z" }, NOW)).toBe("expiring");
  });
  it("active otherwise", () => {
    expect(deriveClientState(base, NOW)).toBe("active");
  });
  it("disabled wins over deploy_failed", () => {
    expect(deriveClientState({ ...base, enabled: false, lastDeployStatus: "failed", assignedNodesCount: 1 }, NOW)).toBe("disabled");
  });
  it("deploy_failed wins over over_quota", () => {
    expect(deriveClientState({ ...base, lastDeployStatus: "failed", assignedNodesCount: 1, dataQuotaBytes: 100, trafficUsedBytes: 500 }, NOW)).toBe("deploy_failed");
  });
  it("over_quota wins over expiring", () => {
    expect(deriveClientState({ ...base, dataQuotaBytes: 100, assignedNodesCount: 1, trafficUsedBytes: 200, expirationRfc3339: "2026-06-07T00:00:00Z" }, NOW)).toBe("over_quota");
  });
  it("not_deployed wins over expiring", () => {
    expect(deriveClientState({ ...base, assignedNodesCount: 1, lastDeployStatus: "pending", expirationRfc3339: "2026-06-07T00:00:00Z" }, NOW)).toBe("not_deployed");
  });
});
