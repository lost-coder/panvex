import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Mock the global toast channel the same way the sibling config tests do.
const toastApi = {
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  withAction: vi.fn(),
  dismiss: vi.fn(),
};
vi.mock("@/app/providers/ToastProvider", () => ({
  useToast: () => toastApi,
}));

// Mock the strategy hooks so the section can be exercised without a
// QueryClient or network — the same isolation strategy ConfigTab.test.tsx
// uses for configHooks.
const putMutate = vi.fn();
const dispatchMutate = vi.fn();
const useTelemtUpdateStrategy = vi.fn();
const useDispatchTelemtUpdate = vi.fn();
vi.mock("./useTelemtUpdateStrategy", () => ({
  useTelemtUpdateStrategy: (agentId: string) => useTelemtUpdateStrategy(agentId),
  usePutTelemtUpdateStrategy: () => ({ mutate: putMutate, isPending: false }),
  useDispatchTelemtUpdate: (agentId: string) => useDispatchTelemtUpdate(agentId),
}));

// Task 14: confirm dialog for the update button — resolves true/false per
// test via mockResolvedValueOnce, mirroring ServerDetailContainer.test.tsx.
const confirmMock = vi.fn();
vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => confirmMock,
}));

import { TelemtUpdateSection } from "./TelemtUpdateSection";

// TelemtUpdateSection renders inside a collapsed-by-default <Fold>; expand
// it so the probe hint / form become reachable.
function openFold() {
  fireEvent.click(screen.getByRole("button", { name: /telemt update/i }));
}

describe("TelemtUpdateSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useDispatchTelemtUpdate.mockReturnValue({ mutate: dispatchMutate, isPending: false });
    confirmMock.mockResolvedValue(true);
  });

  it("shows the probe hint when the agent has reported a probe", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: {
        strategy: null,
        probe: {
          mode: "binary",
          suggested_restart_spec: "systemd:telemt",
          binary_path: "/usr/local/bin/telemt",
          available: true,
          reason: "",
        },
      },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();
    expect(screen.getByText("Auto-detected")).toBeInTheDocument();
    expect(screen.getByText(/systemd:telemt/)).toBeInTheDocument();
    expect(screen.getByText(/\/usr\/local\/bin\/telemt/)).toBeInTheDocument();
    // The raw wire value ("binary") must be localized through the same
    // telemtUpdate.form.mode.* keys the mode <Select> uses, not shown verbatim
    // as "Mode: binary".
    expect(screen.getByText("Mode: Binary")).toBeInTheDocument();
  });

  it("falls back to the raw probe mode string when it isn't a known mode key", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: {
        strategy: null,
        probe: {
          mode: "some_future_mode",
          suggested_restart_spec: "",
          binary_path: "",
          available: true,
          reason: "",
        },
      },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();
    expect(screen.getByText("Mode: some_future_mode")).toBeInTheDocument();
  });

  it("does not render a probe hint when the agent has never reported one", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: { strategy: null, probe: null },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();
    expect(screen.queryByText("Auto-detected")).not.toBeInTheDocument();
  });

  it("'Apply suggested' fills the form from the probe", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: {
        strategy: null,
        probe: {
          mode: "binary",
          suggested_restart_spec: "systemd:telemt",
          binary_path: "/usr/local/bin/telemt",
          available: true,
          reason: "",
        },
      },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();
    fireEvent.click(screen.getByRole("button", { name: "Apply suggested" }));

    expect(screen.getByLabelText("Update mode")).toHaveValue("binary");
    expect(screen.getByLabelText("Process supervisor")).toHaveValue("systemd");
    expect(screen.getByDisplayValue("telemt")).toBeInTheDocument();
    expect(screen.getByDisplayValue("/usr/local/bin/telemt")).toBeInTheDocument();
  });

  it("warns with the localized reason when the probe reports unavailable", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: {
        strategy: null,
        probe: {
          mode: "none",
          suggested_restart_spec: "",
          binary_path: "",
          available: false,
          reason: "no_service_manager_detected",
        },
      },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();
    expect(screen.getByText(/in-place update unavailable/i)).toBeInTheDocument();
    expect(screen.getByText(/no supported process supervisor detected/i)).toBeInTheDocument();
  });

  it("falls back to the raw reason code for an unrecognised probe reason", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: {
        strategy: null,
        probe: {
          mode: "none",
          suggested_restart_spec: "",
          binary_path: "",
          available: false,
          reason: "some_future_reason",
        },
      },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();
    expect(screen.getByText(/some_future_reason/)).toBeInTheDocument();
  });

  it("submits the composed strategy body on save", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: { strategy: null, probe: null },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();

    fireEvent.change(screen.getByLabelText("Update mode"), { target: { value: "binary" } });
    fireEvent.change(screen.getByLabelText("Unit / service name"), {
      target: { value: "telemt" },
    });
    fireEvent.change(screen.getByLabelText("Telemt binary path"), {
      target: { value: "/usr/local/bin/telemt" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

    expect(putMutate).toHaveBeenCalledTimes(1);
    expect(putMutate.mock.calls[0]?.[0]).toEqual({
      mode: "binary",
      restart_spec: "systemd:telemt",
      binary_path: "/usr/local/bin/telemt",
      asset_flavor: "",
    });
  });

  it("submits a custom restart command and the v3 flavor flag", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: { strategy: null, probe: null },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();

    fireEvent.change(screen.getByLabelText("Update mode"), { target: { value: "binary" } });
    fireEvent.change(screen.getByLabelText("Process supervisor"), {
      target: { value: "custom" },
    });
    fireEvent.change(screen.getByLabelText("Restart command"), {
      target: { value: "/usr/local/bin/restart-telemt.sh" },
    });
    fireEvent.change(screen.getByLabelText("Telemt binary path"), {
      target: { value: "/usr/local/bin/telemt" },
    });
    fireEvent.click(screen.getByRole("switch"));
    fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

    expect(putMutate.mock.calls[0]?.[0]).toEqual({
      mode: "binary",
      restart_spec: "command:/usr/local/bin/restart-telemt.sh",
      binary_path: "/usr/local/bin/telemt",
      asset_flavor: "v3",
    });
  });

  it("blocks save and shows a validation message when the unit name is missing", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: { strategy: null, probe: null },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();

    fireEvent.change(screen.getByLabelText("Update mode"), { target: { value: "binary" } });
    fireEvent.change(screen.getByLabelText("Telemt binary path"), {
      target: { value: "/usr/local/bin/telemt" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

    expect(putMutate).not.toHaveBeenCalled();
    expect(screen.getByText("Enter a unit or service name")).toBeInTheDocument();
  });

  it("blocks save and shows a validation message when the binary path is not absolute", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: { strategy: null, probe: null },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();

    fireEvent.change(screen.getByLabelText("Update mode"), { target: { value: "binary" } });
    fireEvent.change(screen.getByLabelText("Unit / service name"), {
      target: { value: "telemt" },
    });
    fireEvent.change(screen.getByLabelText("Telemt binary path"), {
      target: { value: "usr/local/bin/telemt" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

    expect(putMutate).not.toHaveBeenCalled();
    expect(
      screen.getByText('Binary path must be absolute (start with "/")'),
    ).toBeInTheDocument();
  });

  it("seeds the form from an already-configured strategy", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: {
        strategy: {
          mode: "binary",
          restart_spec: "openrc:telemt",
          binary_path: "/opt/telemt/telemt",
          asset_flavor: "v3",
        },
        probe: null,
      },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();

    expect(screen.getByLabelText("Update mode")).toHaveValue("binary");
    expect(screen.getByLabelText("Process supervisor")).toHaveValue("openrc");
    expect(screen.getByDisplayValue("telemt")).toBeInTheDocument();
    expect(screen.getByDisplayValue("/opt/telemt/telemt")).toBeInTheDocument();
    expect(screen.getByRole("switch")).toHaveAttribute("aria-checked", "true");
  });

  it("shows a loading state while the query is pending", () => {
    useTelemtUpdateStrategy.mockReturnValue({ data: undefined, isLoading: true, isError: false });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();
    expect(screen.getByText("Loading update strategy…")).toBeInTheDocument();
  });

  it("shows an error state when the query fails", () => {
    useTelemtUpdateStrategy.mockReturnValue({ data: undefined, isLoading: false, isError: true });
    render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" />);
    openFold();
    expect(screen.getByText("Failed to load the update strategy")).toBeInTheDocument();
  });

  describe("version badge + update action (Task 14)", () => {
    const binaryStrategy = {
      mode: "binary" as const,
      restart_spec: "systemd:telemt",
      binary_path: "/usr/local/bin/telemt",
      asset_flavor: "",
    };
    const availableProbe = {
      mode: "binary",
      suggested_restart_spec: "systemd:telemt",
      binary_path: "/usr/local/bin/telemt",
      available: true,
      reason: "",
    };

    it("shows the 'current → latest' badge (even collapsed) when a newer release is known", () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: binaryStrategy, probe: availableProbe },
        isLoading: false,
        isError: false,
      });
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" latestVersion="3.4.25" />);
      // Fold is collapsed by default — the badge lives in the header
      // (Fold's rightHint), so it must be visible without opening it.
      expect(screen.getByText("Telemt 3.4.10 → 3.4.25")).toBeInTheDocument();
    });

    it("shows no badge when the current (Telemt) version is unknown, even with a newer release known", () => {
      // Task 14 fix: currentVersion is the RAW Telemt-reported version with
      // no panvex-agent fallback (see transforms/servers.ts telemtVersion) —
      // "" means Telemt hasn't reported system_info yet (fresh enroll /
      // partial data), which must never be compared against a real release.
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: binaryStrategy, probe: availableProbe },
        isLoading: false,
        isError: false,
      });
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="" latestVersion="3.4.25" />);
      expect(screen.queryByText(/→/)).not.toBeInTheDocument();
      openFold();
      expect(screen.queryByRole("button", { name: /Update Telemt/ })).not.toBeInTheDocument();
    });

    it("shows no badge and no update action when already on the latest known version", () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: binaryStrategy, probe: availableProbe },
        isLoading: false,
        isError: false,
      });
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.25" latestVersion="3.4.25" />);
      expect(screen.queryByText(/→/)).not.toBeInTheDocument();
      openFold();
      expect(screen.queryByRole("button", { name: /Update Telemt/ })).not.toBeInTheDocument();
    });

    it("enables the update button when strategy + probe both support an in-place update", () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: binaryStrategy, probe: availableProbe },
        isLoading: false,
        isError: false,
      });
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" latestVersion="3.4.25" />);
      openFold();
      expect(screen.getByRole("button", { name: "Update Telemt to 3.4.25" })).toBeEnabled();
    });

    it("disables the update button when no strategy has been configured", () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: null, probe: null },
        isLoading: false,
        isError: false,
      });
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" latestVersion="3.4.25" />);
      openFold();
      expect(screen.getByRole("button", { name: "Update Telemt to 3.4.25" })).toBeDisabled();
    });

    it("disables the update button when the configured strategy mode is not binary (e.g. none)", () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: { ...binaryStrategy, mode: "none" as const }, probe: availableProbe },
        isLoading: false,
        isError: false,
      });
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" latestVersion="3.4.25" />);
      openFold();
      expect(screen.getByRole("button", { name: "Update Telemt to 3.4.25" })).toBeDisabled();
    });

    it("disables the update button when the probe reports the update unavailable", () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: binaryStrategy, probe: { ...availableProbe, available: false, reason: "no_service_manager_detected" } },
        isLoading: false,
        isError: false,
      });
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" latestVersion="3.4.25" />);
      openFold();
      expect(screen.getByRole("button", { name: "Update Telemt to 3.4.25" })).toBeDisabled();
    });

    it("shows the docker hint instead of an update button when the strategy mode is docker", () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: { ...binaryStrategy, mode: "docker" as const }, probe: null },
        isLoading: false,
        isError: false,
      });
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" latestVersion="3.4.25" />);
      openFold();
      expect(screen.getByText("docker compose pull && docker compose up -d")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Update Telemt/ })).not.toBeInTheDocument();
    });

    it("dispatches the resolved version after the operator confirms", async () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: binaryStrategy, probe: availableProbe },
        isLoading: false,
        isError: false,
      });
      confirmMock.mockResolvedValueOnce(true);
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" latestVersion="3.4.25" />);
      openFold();

      fireEvent.click(screen.getByRole("button", { name: "Update Telemt to 3.4.25" }));

      await waitFor(() => expect(dispatchMutate).toHaveBeenCalledWith("3.4.25"));
      expect(confirmMock).toHaveBeenCalledWith(
        expect.objectContaining({ title: "Update Telemt?", confirmLabel: "Update" }),
      );
    });

    it("does not dispatch when the operator cancels the confirm dialog", async () => {
      useTelemtUpdateStrategy.mockReturnValue({
        data: { strategy: binaryStrategy, probe: availableProbe },
        isLoading: false,
        isError: false,
      });
      confirmMock.mockResolvedValueOnce(false);
      render(<TelemtUpdateSection agentId="agent-1" currentVersion="3.4.10" latestVersion="3.4.25" />);
      openFold();

      fireEvent.click(screen.getByRole("button", { name: "Update Telemt to 3.4.25" }));

      await waitFor(() => expect(confirmMock).toHaveBeenCalled());
      expect(dispatchMutate).not.toHaveBeenCalled();
    });
  });
});
