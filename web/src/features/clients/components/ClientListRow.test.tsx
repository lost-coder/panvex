import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ClientListItem } from "@/ui";
import { ClientListRow } from "./ClientListRow";

// Render test for ClientListRow's unified ClientStateBadge. The row renders a
// quiet aria-hidden ✓ chip for the healthy `active` state (no visible status
// WORD) and a loud StatusPill carrying the localized label for problem states.
//
// i18n is bootstrapped globally in vitest.setup.ts (default language "en", real
// resources), so the status label resolves to English:
//   clients:statusBadge.expired → "Expired"
//   clients:statusBadge.active  → "Active" (never rendered as text — the active
//                                 chip is an aria-hidden glyph only)

const NOW = Date.parse("2026-06-04T00:00:00Z");

function makeClient(overrides: Partial<ClientListItem>): ClientListItem {
  return {
    id: "c1",
    name: "alice",
    enabled: true,
    assignedNodesCount: 0,
    expirationRfc3339: "",
    trafficUsedBytes: 0,
    uniqueIpsUsed: 0,
    activeTcpConns: 0,
    dataQuotaBytes: 0,
    lastDeployStatus: "succeeded",
    ...overrides,
  };
}

describe("ClientListRow / unified status badge", () => {
  it("renders the localized pill label for an expired client", () => {
    render(
      <ClientListRow
        client={makeClient({ expirationRfc3339: "2020-01-01T00:00:00Z" })}
        nowMs={NOW}
      />,
    );

    expect(screen.getByText("Expired")).toBeInTheDocument();
  });

  it("renders a quiet ✓ chip (no status word) for an active client", () => {
    render(<ClientListRow client={makeClient({})} nowMs={NOW} />);

    // Healthy state shows an aria-hidden glyph chip, not the "Active" word.
    expect(screen.queryByText("Active")).not.toBeInTheDocument();
    // The client name still renders.
    expect(screen.getByText("alice")).toBeInTheDocument();
  });
});
