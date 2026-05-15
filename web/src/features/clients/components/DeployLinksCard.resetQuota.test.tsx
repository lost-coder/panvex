// Reset-quota Phase 2 component-level coverage for the new affordance
// glued onto the per-agent Quota cell:
//
//   - idle (no state)              → trigger button visible, no inline msg.
//   - pending                      → "Resetting…" inline spinner.
//   - succeeded                    → trigger button visible again, no msg
//                                    (success toast comes from the
//                                    container, not the cell).
//   - failed + unsupported_telemt  → translated "Reset unavailable…".
//   - failed + read_only_telemt    → translated "Telemt is in read-only…".
//   - failed + generic             → translated "Reset failed: {{error}}".
//
// Permission gate (viewer role) is covered by *not* passing the
// `onResetQuota` callback — when the prop is undefined the cell hides
// the affordance entirely, which is exactly what the container does
// for viewer-role sessions.

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { ResetOutcome } from "@/features/clients/hooks/useResetQuota";
import type { ClientDeploymentData } from "@/shared/api/types-pages/clients";
import { DeployLinksCard } from "./DeployLinksCard";

function makeDeployment(
  overrides: Partial<ClientDeploymentData> = {},
): ClientDeploymentData {
  return {
    agentId: "agent-1",
    desiredOperation: "client.create",
    status: "succeeded",
    lastError: "",
    links: { classic: [], secure: [], tls: [] },
    lastAppliedAtUnix: 0,
    quotaUsedBytes: 100_000_000,
    quotaLastResetUnix: 0,
    panelLastResetUnix: 0,
    quotaResetDrift: false,
    ...overrides,
  };
}

describe("DeployLinksCard reset-quota affordance", () => {
  it("hides the Reset button when onResetQuota is undefined (viewer gate)", () => {
    render(
      <DeployLinksCard
        deployments={[makeDeployment()]}
        dataQuotaBytes={500_000_000}
      />,
    );
    expect(screen.queryByLabelText("Reset")).toBeNull();
  });

  it("renders the Reset button and fires the callback when clicked", async () => {
    const onReset = vi.fn();
    const user = userEvent.setup();
    render(
      <DeployLinksCard
        deployments={[makeDeployment()]}
        dataQuotaBytes={500_000_000}
        onResetQuota={onReset}
      />,
    );

    const btn = screen.getByLabelText("Reset");
    await user.click(btn);
    expect(onReset).toHaveBeenCalledWith("agent-1");
  });

  it("shows the 'Resetting…' inline spinner while a reset is pending", () => {
    const pending: Record<string, ResetOutcome> = {
      "agent-1": { kind: "pending" },
    };
    render(
      <DeployLinksCard
        deployments={[makeDeployment()]}
        dataQuotaBytes={500_000_000}
        onResetQuota={vi.fn()}
        resetStates={pending}
      />,
    );
    expect(screen.getByText("Resetting…")).toBeInTheDocument();
    // The trigger button is hidden while the spinner is visible — the
    // operator can't double-fire the same reset.
    expect(screen.queryByLabelText("Reset")).toBeNull();
  });

  it("returns to the idle trigger when the job succeeded (no inline msg)", () => {
    const states: Record<string, ResetOutcome> = {
      "agent-1": { kind: "success" },
    };
    render(
      <DeployLinksCard
        deployments={[makeDeployment()]}
        dataQuotaBytes={500_000_000}
        onResetQuota={vi.fn()}
        resetStates={states}
      />,
    );
    expect(screen.getByLabelText("Reset")).toBeInTheDocument();
    expect(screen.queryByText(/Reset failed/)).toBeNull();
    expect(screen.queryByText(/Reset unavailable/)).toBeNull();
  });

  it("renders the unsupported-telemt inline message on failed + unsupported", () => {
    const states: Record<string, ResetOutcome> = {
      "agent-1": { kind: "unsupported" },
    };
    render(
      <DeployLinksCard
        deployments={[makeDeployment()]}
        dataQuotaBytes={500_000_000}
        onResetQuota={vi.fn()}
        resetStates={states}
      />,
    );
    expect(
      screen.getByText(/Reset unavailable on this node \(Telemt < 3\.4\.6\)/),
    ).toBeInTheDocument();
  });

  it("renders the read-only-telemt inline message on failed + read_only", () => {
    const states: Record<string, ResetOutcome> = {
      "agent-1": { kind: "readonly" },
    };
    render(
      <DeployLinksCard
        deployments={[makeDeployment()]}
        dataQuotaBytes={500_000_000}
        onResetQuota={vi.fn()}
        resetStates={states}
      />,
    );
    expect(
      screen.getByText(/Telemt is in read-only mode on this node/),
    ).toBeInTheDocument();
  });

  it("renders the generic-failed inline message with the result_text payload", () => {
    const states: Record<string, ResetOutcome> = {
      "agent-1": { kind: "failed", error: "telemt: 502 bad gateway" },
    };
    render(
      <DeployLinksCard
        deployments={[makeDeployment()]}
        dataQuotaBytes={500_000_000}
        onResetQuota={vi.fn()}
        resetStates={states}
      />,
    );
    expect(
      screen.getByText(/Reset failed: telemt: 502 bad gateway/),
    ).toBeInTheDocument();
  });

  it("calls onDismissResetState when the operator clicks the dismiss button", async () => {
    const states: Record<string, ResetOutcome> = {
      "agent-1": { kind: "failed", error: "boom" },
    };
    const onDismiss = vi.fn();
    const user = userEvent.setup();
    render(
      <DeployLinksCard
        deployments={[makeDeployment()]}
        dataQuotaBytes={500_000_000}
        onResetQuota={vi.fn()}
        resetStates={states}
        onDismissResetState={onDismiss}
      />,
    );
    await user.click(screen.getByLabelText("dismiss"));
    expect(onDismiss).toHaveBeenCalledWith("agent-1");
  });

  it("renders the Reset button even when no quota is configured (counter still resettable)", () => {
    // dataQuotaBytes === 0 doesn't mean the cell collapses to em-dash
    // anymore when the operator could still fire a reset. The counter
    // is tracked Telemt-side regardless of whether the panel has a
    // quota cap configured.
    render(
      <DeployLinksCard
        deployments={[makeDeployment({ quotaUsedBytes: 0 })]}
        dataQuotaBytes={0}
        onResetQuota={vi.fn()}
      />,
    );
    expect(screen.getByLabelText("Reset")).toBeInTheDocument();
  });
});
