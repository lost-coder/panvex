import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";
import { ConnectStep } from "./ConnectStep";

function makeProps(overrides: Partial<EnrollmentWizardProps> = {}): EnrollmentWizardProps {
  return {
    step: 3,
    fleetGroups: [],
    nodeName: "edge-1",
    selectedFleetGroup: "fg-1",
    tokenTtl: 3600,
    onNodeNameChange: vi.fn(),
    onFleetGroupChange: vi.fn(),
    onTokenTtlChange: vi.fn(),
    onGenerateToken: vi.fn(),
    installCommand: "curl …",
    tokenValue: "tok_0123456789abcdef",
    tokenExpiresInSecs: 600,
    connectionStatus: { bootstrap: "done", grpcConnect: "waiting", firstData: "pending" },
    onCancel: vi.fn(),
    onInstallConfirm: vi.fn(),
    onBack: vi.fn(),
    onViewDetails: vi.fn(),
    ...overrides,
  } as EnrollmentWizardProps;
}

describe("ConnectStep error recovery", () => {
  it("renders the polling error with retry and enrollment-log actions", async () => {
    const onRetryPolling = vi.fn();
    const onViewAttempts = vi.fn();
    render(
      <ConnectStep
        {...makeProps({ error: "Probe failed 3× in a row: network down" })}
        onRetryPolling={onRetryPolling}
        onViewAttempts={onViewAttempts}
      />,
    );
    expect(screen.getByText(/Probe failed 3/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(onRetryPolling).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole("button", { name: "Open enrollment log" }));
    expect(onViewAttempts).toHaveBeenCalledTimes(1);
  });

  it("renders no error block when error is absent", () => {
    render(<ConnectStep {...makeProps()} />);
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });
});
