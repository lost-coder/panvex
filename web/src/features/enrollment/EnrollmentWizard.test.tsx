import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { EnrollmentWizard } from "./EnrollmentWizard";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";

const baseProps: EnrollmentWizardProps = {
  step: 1,
  fleetGroups: [{ id: "default", name: "Default", nodeCount: 3 }],
  nodeName: "",
  selectedFleetGroup: "default",
  tokenTtl: 3600,
  onNodeNameChange: vi.fn(),
  onFleetGroupChange: vi.fn(),
  onTokenTtlChange: vi.fn(),
  onGenerateToken: vi.fn(),
  installCommand: "curl -sSL https://panvex.io/install | sudo bash -s -- --token=abc123",
  tokenValue: "tok-abcdef1234567890abcdef",
  tokenExpiresInSecs: 3600,
  onInstallConfirm: vi.fn(),
  onBack: vi.fn(),
  connectionStatus: {
    bootstrap: "pending",
    grpcConnect: "pending",
    firstData: "pending",
  },
  onViewDetails: vi.fn(),
  onCancel: vi.fn(),
};

describe("EnrollmentWizard", () => {
  it("renders step 1 — configure", () => {
    render(<EnrollmentWizard {...baseProps} step={1} />);
    expect(screen.getByText("Add server node")).toBeInTheDocument();
    expect(screen.getByText(/one-shot token/i)).toBeInTheDocument();
    expect(screen.getByPlaceholderText("e.g. prod-eu-west-1")).toBeInTheDocument();
  });

  it("shows inline validation error when node name is empty and submit is attempted", async () => {
    const user = userEvent.setup();
    const onGenerateToken = vi.fn();
    render(
      <EnrollmentWizard {...baseProps} step={1} nodeName="" onGenerateToken={onGenerateToken} />,
    );
    const btn = screen.getByRole("button", { name: /generate token/i });
    expect(btn).toBeEnabled();
    await user.click(btn);
    expect(onGenerateToken).not.toHaveBeenCalled();
    expect(screen.getByText(/node name is required/i)).toBeInTheDocument();
  });

  it("enables generate button when node name is set", () => {
    render(<EnrollmentWizard {...baseProps} step={1} nodeName="prod-eu" />);
    const btn = screen.getByRole("button", { name: /generate token/i });
    expect(btn).toBeEnabled();
  });

  it("applies aria-pressed to TTL preset toggle buttons (P2-UX-08)", () => {
    render(<EnrollmentWizard {...baseProps} step={1} nodeName="x" tokenTtl={3600} />);
    const oneHour = screen.getByRole("button", { name: /1 hour/i });
    const sixHours = screen.getByRole("button", { name: /6 hours/i });
    expect(oneHour).toHaveAttribute("aria-pressed", "true");
    expect(sixHours).toHaveAttribute("aria-pressed", "false");
  });

  it("calls onGenerateToken when button clicked", async () => {
    const user = userEvent.setup();
    const onGenerateToken = vi.fn();
    render(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        nodeName="prod-eu"
        onGenerateToken={onGenerateToken}
      />,
    );

    await user.click(screen.getByRole("button", { name: /generate token/i }));
    expect(onGenerateToken).toHaveBeenCalledOnce();
  });

  it("calls onNodeNameChange when typing", async () => {
    const user = userEvent.setup();
    const onNodeNameChange = vi.fn();
    render(<EnrollmentWizard {...baseProps} step={1} onNodeNameChange={onNodeNameChange} />);

    const input = screen.getByPlaceholderText("e.g. prod-eu-west-1");
    await user.type(input, "a");
    expect(onNodeNameChange).toHaveBeenCalled();
  });

  it("renders step 2 — install", () => {
    render(<EnrollmentWizard {...baseProps} step={2} />);
    expect(screen.getByText(/run this command/i)).toBeInTheDocument();
    expect(screen.getByText(/install command/i)).toBeInTheDocument();
  });

  it("shows install command in step 2", () => {
    render(<EnrollmentWizard {...baseProps} step={2} />);
    expect(screen.getAllByText(/curl/i).length).toBeGreaterThanOrEqual(1);
  });

  it("renders step 3 — connect with pending statuses", () => {
    render(<EnrollmentWizard {...baseProps} step={3} />);
    expect(screen.getByText(/waiting for the agent to come online/i)).toBeInTheDocument();
    expect(screen.getByText("Bootstrap")).toBeInTheDocument();
    expect(screen.getByText("Gateway connected")).toBeInTheDocument();
    expect(screen.getByText("First snapshot")).toBeInTheDocument();
  });

  it("renders step 3 — connected state with auto-redirect hint", () => {
    const onViewDetails = vi.fn();
    render(
      <EnrollmentWizard
        {...baseProps}
        step={3}
        connectionStatus={{
          bootstrap: "done",
          grpcConnect: "done",
          firstData: "done",
        }}
        connectedAgent={{
          id: "agent-001",
          version: "v1.0.0",
          fleetGroup: "Default",
          certExpiresAt: "2026-05-15",
        }}
        onViewDetails={onViewDetails}
      />,
    );
    // Phase-7 wizard auto-redirects when every stage is done; the
    // inline hint + agent id surface the transient state without a
    // separate summary screen.
    expect(screen.getByText(/redirecting to the server page/i)).toBeInTheDocument();
    expect(screen.getByText(/agent-001/)).toBeInTheDocument();
  });

  it("shows error in step 1", () => {
    render(<EnrollmentWizard {...baseProps} step={1} error="Token generation failed" />);
    expect(screen.getByText("Token generation failed")).toBeInTheDocument();
  });

  it("shows loading state in step 1", () => {
    render(<EnrollmentWizard {...baseProps} step={1} nodeName="test" loading={true} />);
    expect(screen.getByText(/generating/i)).toBeInTheDocument();
  });
});
