// Reset-quota Phase 1 component-level coverage for the per-agent
// "Quota" cell embedded in the Deployments & Links card. The cell
// has three render branches:
//
//   1. quota configured + non-zero last-reset → progress bar + bytes
//      label + relative "Last reset: …" line.
//   2. quota configured + last-reset == 0     → progress bar + bytes
//      label + "Never reset" line.
//   3. quota absent (data_quota_bytes == 0)  → "X used (no quota)" or
//      em-dash when neither quota nor used has signal.
//
// We deliberately drive the test through the parent card rather than
// reaching for QuotaCell directly — that way wiring regressions in
// DeployLinksCard (e.g. dropping the prop on the way down) get caught
// alongside cell-rendering regressions.

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

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
    quotaUsedBytes: 0,
    quotaLastResetUnix: 0,
    ...overrides,
  };
}

describe("DeployLinksCard QuotaCell", () => {
  it("renders a quota progress bar + used/quota label + relative reset when quota and history are set", () => {
    // Anchor the relative-time label by using a recent epoch — exact
    // formatting comes from `formatAge`, so we assert on the prefix
    // emitted by the i18n template instead of a brittle substring.
    const tenMinutesAgo = Math.floor(Date.now() / 1000) - 10 * 60;
    render(
      <DeployLinksCard
        deployments={[
          makeDeployment({
            quotaUsedBytes: 512_000_000,
            quotaLastResetUnix: tenMinutesAgo,
          }),
        ]}
        dataQuotaBytes={1_000_000_000}
      />,
    );

    // Progress bar primitive renders the percentage with one decimal —
    // 512M / 1000M ≈ 51.2 %. formatBytes() crosses the GB threshold
    // strictly above 1e9, so 1_000_000_000 still renders as MB.
    expect(screen.getByText("51.2%")).toBeInTheDocument();
    expect(
      screen.getByText(/Used: 512\.0 MB \/ 1000\.0 MB/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Last reset:/)).toBeInTheDocument();
  });

  it("renders 'Never reset' when quota is configured but the agent has never reset", () => {
    render(
      <DeployLinksCard
        deployments={[
          makeDeployment({
            quotaUsedBytes: 100_000_000,
            quotaLastResetUnix: 0,
          }),
        ]}
        dataQuotaBytes={500_000_000}
      />,
    );

    expect(screen.getByText(/Used: 100\.0 MB \/ 500\.0 MB/)).toBeInTheDocument();
    expect(screen.getByText(/Never reset/)).toBeInTheDocument();
    // The progress-bar primitive emits the percentage label even when
    // we never reset — that signal is independent of reset history.
    expect(screen.getByText("20.0%")).toBeInTheDocument();
  });

  it("renders the 'used (no quota)' line when dataQuotaBytes is 0 and used > 0", () => {
    render(
      <DeployLinksCard
        deployments={[
          makeDeployment({
            quotaUsedBytes: 42_000_000,
            quotaLastResetUnix: 0,
          }),
        ]}
        dataQuotaBytes={0}
      />,
    );

    expect(
      screen.getByText(/42\.0 MB used \(no quota\)/),
    ).toBeInTheDocument();
    // No progress bar in the no-quota branch — assert absence by
    // looking for the percentage label that the primitive always emits.
    expect(screen.queryByText(/^\d+\.\d%$/)).toBeNull();
  });

  it("collapses to an em-dash when there is neither quota nor used bytes", () => {
    render(
      <DeployLinksCard
        deployments={[
          makeDeployment({ quotaUsedBytes: 0, quotaLastResetUnix: 0 }),
        ]}
        dataQuotaBytes={0}
      />,
    );

    // The cell sits under the "Quota" header — that's the affordance
    // that tells operators "this row has nothing to show".
    expect(screen.getByText("Quota")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
