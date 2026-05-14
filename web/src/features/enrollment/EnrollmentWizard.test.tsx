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

  // ── PR-3b: transport-mode picker, dial_address field, source toggle ──

  it("hides the mode picker when mode/onModeChange are not threaded", () => {
    render(<EnrollmentWizard {...baseProps} step={1} />);
    // Back-compat: existing AddServerContainer call sites without mode
    // state must keep rendering the inbound-only flow unchanged.
    expect(screen.queryByText(/agent connects to panel/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/panel connects to agent/i)).not.toBeInTheDocument();
  });

  it("renders the mode picker when mode + onModeChange are provided", () => {
    const onModeChange = vi.fn();
    render(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        mode="inbound"
        onModeChange={onModeChange}
      />,
    );
    expect(screen.getByRole("radio", { name: /agent connects to panel/i })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /panel connects to agent/i })).toBeInTheDocument();
    // Caption reflects the selected mode.
    expect(screen.getByText(/agent dials the panel/i)).toBeInTheDocument();
  });

  it("shows dial_address only when mode === outbound", () => {
    const { rerender } = render(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        mode="inbound"
        onModeChange={vi.fn()}
        dialAddress=""
        onDialAddressChange={vi.fn()}
      />,
    );
    expect(screen.queryByPlaceholderText(/vps\.example\.com:8443/i)).not.toBeInTheDocument();
    // Token-lifetime presets visible for inbound (TTL belongs to the
    // enrollment-token flow; outbound uses a fixed 5-min bootstrap).
    expect(screen.getByRole("button", { name: /1 hour/i })).toBeInTheDocument();

    rerender(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        mode="outbound"
        onModeChange={vi.fn()}
        dialAddress=""
        onDialAddressChange={vi.fn()}
      />,
    );
    expect(screen.getByPlaceholderText(/vps\.example\.com:8443/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /1 hour/i })).not.toBeInTheDocument();
  });

  it("validates dial_address shape before invoking onGenerateToken", async () => {
    const user = userEvent.setup();
    const onGenerateToken = vi.fn();
    render(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        nodeName="edge-01"
        mode="outbound"
        onModeChange={vi.fn()}
        dialAddress=""
        onDialAddressChange={vi.fn()}
        onGenerateToken={onGenerateToken}
      />,
    );
    await user.click(screen.getByRole("button", { name: /generate token/i }));
    expect(onGenerateToken).not.toHaveBeenCalled();
    expect(screen.getByText(/dial address is required/i)).toBeInTheDocument();
  });

  it("rejects dial_address without host:port", async () => {
    const user = userEvent.setup();
    const onGenerateToken = vi.fn();
    render(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        nodeName="edge-01"
        mode="outbound"
        onModeChange={vi.fn()}
        dialAddress="just-a-host"
        onDialAddressChange={vi.fn()}
        onGenerateToken={onGenerateToken}
      />,
    );
    await user.click(screen.getByRole("button", { name: /generate token/i }));
    expect(onGenerateToken).not.toHaveBeenCalled();
    expect(screen.getByText(/must be host:port/i)).toBeInTheDocument();
  });

  it("renders the source toggle inside Advanced when the operator opens it", async () => {
    const user = userEvent.setup();
    render(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        mode="inbound"
        onModeChange={vi.fn()}
        scriptSource="panel"
        onScriptSourceChange={vi.fn()}
        advancedOptions={{
          telemtUrl: "http://127.0.0.1:9091",
          telemtMetricsUrl: "http://127.0.0.1:8081",
          telemtAuth: "",
          insecureTransport: false,
        }}
        onAdvancedOptionsChange={vi.fn()}
      />,
    );
    // Default: Advanced is collapsed, source toggle hidden.
    expect(screen.queryByRole("button", { name: /^panel$/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^advanced$/i }));
    // Both source options are now visible and selectable — PR-3c removes
    // the availability gate, so Panel is never disabled.
    expect(screen.getByRole("button", { name: /^panel$/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /^github$/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /^panel$/i })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("calls onModeChange when the operator switches transport", async () => {
    const user = userEvent.setup();
    const onModeChange = vi.fn();
    render(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        mode="inbound"
        onModeChange={onModeChange}
        onDialAddressChange={vi.fn()}
      />,
    );
    await user.click(screen.getByRole("radio", { name: /panel connects to agent/i }));
    expect(onModeChange).toHaveBeenCalledWith("outbound");
  });

  it("keeps Telemt and insecure-transport knobs hidden until Advanced is opened", async () => {
    const user = userEvent.setup();
    render(
      <EnrollmentWizard
        {...baseProps}
        step={1}
        advancedOptions={{
          telemtUrl: "http://127.0.0.1:9091",
          telemtMetricsUrl: "http://127.0.0.1:8081",
          telemtAuth: "",
          insecureTransport: false,
        }}
        onAdvancedOptionsChange={vi.fn()}
      />,
    );
    // Default state — none of the niche knobs surface in the main form.
    expect(screen.queryByLabelText(/telemt api url/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: /allow plaintext/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^advanced$/i }));
    expect(screen.getByLabelText(/telemt api url/i)).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /allow plaintext/i })).toBeInTheDocument();
  });
});
