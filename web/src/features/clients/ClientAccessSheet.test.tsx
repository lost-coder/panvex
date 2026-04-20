import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ClientAccessSheet } from "./ClientAccessSheet";
import type { FleetGroupOption, NodeOption } from "@/shared/api/types-pages/pages";

const fleetGroups: FleetGroupOption[] = [
  { id: "fg-1", name: "prod" },
  { id: "fg-2", name: "staging" },
];

const nodes: NodeOption[] = [{ id: "n-1", name: "edge-01", fleetGroup: "fg-1", status: "ok" }];

describe("ClientAccessSheet a11y (P2-FE-07 / M-F6)", () => {
  it("exposes aria-expanded + aria-controls on the Fine-tune toggle", async () => {
    const user = userEvent.setup();
    render(
      <ClientAccessSheet
        fleetGroups={fleetGroups}
        nodes={nodes}
        selectedFleetGroupIds={[]}
        selectedNodeIds={[]}
        onFleetGroupsChange={vi.fn()}
        onNodesChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    );

    const toggle = screen.getByRole("button", {
      name: /fine-tune individual nodes/i,
    });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(toggle).toHaveAttribute("aria-controls", "client-access-finetune-section");

    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");

    const region = document.getElementById("client-access-finetune-section");
    expect(region).not.toBeNull();
  });
});
