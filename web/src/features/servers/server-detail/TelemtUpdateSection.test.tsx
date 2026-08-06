import { fireEvent, render, screen } from "@testing-library/react";
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
const useTelemtUpdateStrategy = vi.fn();
vi.mock("./useTelemtUpdateStrategy", () => ({
  useTelemtUpdateStrategy: (agentId: string) => useTelemtUpdateStrategy(agentId),
  usePutTelemtUpdateStrategy: () => ({ mutate: putMutate, isPending: false }),
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
    render(<TelemtUpdateSection agentId="agent-1" />);
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
    render(<TelemtUpdateSection agentId="agent-1" />);
    openFold();
    expect(screen.getByText("Mode: some_future_mode")).toBeInTheDocument();
  });

  it("does not render a probe hint when the agent has never reported one", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: { strategy: null, probe: null },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" />);
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
    render(<TelemtUpdateSection agentId="agent-1" />);
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
    render(<TelemtUpdateSection agentId="agent-1" />);
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
    render(<TelemtUpdateSection agentId="agent-1" />);
    openFold();
    expect(screen.getByText(/some_future_reason/)).toBeInTheDocument();
  });

  it("submits the composed strategy body on save", () => {
    useTelemtUpdateStrategy.mockReturnValue({
      data: { strategy: null, probe: null },
      isLoading: false,
      isError: false,
    });
    render(<TelemtUpdateSection agentId="agent-1" />);
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
    render(<TelemtUpdateSection agentId="agent-1" />);
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
    render(<TelemtUpdateSection agentId="agent-1" />);
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
    render(<TelemtUpdateSection agentId="agent-1" />);
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
    render(<TelemtUpdateSection agentId="agent-1" />);
    openFold();

    expect(screen.getByLabelText("Update mode")).toHaveValue("binary");
    expect(screen.getByLabelText("Process supervisor")).toHaveValue("openrc");
    expect(screen.getByDisplayValue("telemt")).toBeInTheDocument();
    expect(screen.getByDisplayValue("/opt/telemt/telemt")).toBeInTheDocument();
    expect(screen.getByRole("switch")).toHaveAttribute("aria-checked", "true");
  });

  it("shows a loading state while the query is pending", () => {
    useTelemtUpdateStrategy.mockReturnValue({ data: undefined, isLoading: true, isError: false });
    render(<TelemtUpdateSection agentId="agent-1" />);
    openFold();
    expect(screen.getByText("Loading update strategy…")).toBeInTheDocument();
  });

  it("shows an error state when the query fails", () => {
    useTelemtUpdateStrategy.mockReturnValue({ data: undefined, isLoading: false, isError: true });
    render(<TelemtUpdateSection agentId="agent-1" />);
    openFold();
    expect(screen.getByText("Failed to load the update strategy")).toBeInTheDocument();
  });
});
