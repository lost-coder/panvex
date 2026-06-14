import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AgentConfig } from "@/shared/api/schemas/config";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

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

// Mock the config hooks so the tab can be exercised without a QueryClient or
// network — the same isolation strategy the feature's component tests use.
const putMutate = vi.fn();
const applyMutateAsync = vi.fn().mockResolvedValue({ applied: 1, failed: "", error: "" });
const useAgentConfig = vi.fn();
vi.mock("@/features/servers/config/configHooks", () => ({
  useAgentConfig: (id: string) => useAgentConfig(id),
  usePutAgentConfig: () => ({ mutate: putMutate, isPending: false }),
  useApplyAgentConfig: () => ({ mutateAsync: applyMutateAsync, isPending: false }),
}));

import { ConfigTab } from "./ConfigTab";

const server = { id: "agent-7", name: "edge-1" } as ServerDetailPageProps["server"];

function makeConfig(overrides: Partial<AgentConfig> = {}): AgentConfig {
  return {
    override: { censorship: { tls_domain: "old.example.com" } },
    effective: { censorship: { tls_domain: "old.example.com" } },
    observed: {},
    drift: { status: "drifted", fields: ["censorship.tls_domain"] },
    ...overrides,
  };
}

describe("ConfigTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAgentConfig.mockReturnValue({
      data: makeConfig(),
      isLoading: false,
      isError: false,
    });
    // jsdom lacks <dialog> modal methods used by ApplyConfigButton's confirm.
    HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
      this.open = true;
    });
    HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
      this.open = false;
    });
  });

  it("renders the drift badge with the drifted status and the diverging field", () => {
    render(<ConfigTab server={server} />);
    expect(screen.getByText("Drifted")).toBeInTheDocument();
    expect(screen.getByText("Diverging fields")).toBeInTheDocument();
    // The diverging field is listed as a badge.
    expect(screen.getByText("censorship.tls_domain")).toBeInTheDocument();
  });

  it("seeds the editor from the override", () => {
    render(<ConfigTab server={server} />);
    expect(screen.getByDisplayValue("old.example.com")).toBeInTheDocument();
  });

  it("saves the unflattened sections when Save is clicked", () => {
    render(<ConfigTab server={server} />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "new.example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(putMutate).toHaveBeenCalledTimes(1);
    // Body is the nested-sections shape produced by unflattenPaths.
    expect(putMutate.mock.calls[0]?.[0]).toEqual({
      censorship: { tls_domain: "new.example.com" },
    });
  });

  it("toasts on a successful save", () => {
    putMutate.mockImplementation((_body, opts?: { onSuccess?: () => void }) =>
      opts?.onSuccess?.(),
    );
    render(<ConfigTab server={server} />);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(toastApi.success).toHaveBeenCalledWith("Configuration saved");
  });

  it("renders the Apply button", () => {
    render(<ConfigTab server={server} />);
    expect(screen.getByRole("button", { name: "Apply to node" })).toBeInTheDocument();
  });

  it("shows a loading state while the query is pending", () => {
    useAgentConfig.mockReturnValue({ data: undefined, isLoading: true, isError: false });
    render(<ConfigTab server={server} />);
    expect(screen.getByText("Loading…")).toBeInTheDocument();
  });

  it("shows an error state when the query fails", () => {
    useAgentConfig.mockReturnValue({ data: undefined, isLoading: false, isError: true });
    render(<ConfigTab server={server} />);
    expect(screen.getByText("Request failed")).toBeInTheDocument();
  });

  it("applies the persisted override on confirm (restart field → warning dialog)", async () => {
    render(<ConfigTab server={server} />);
    // The override holds a restart-only field (censorship.tls_domain), so
    // Apply opens the restart-warning confirm before pushing.
    fireEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    expect(screen.getByText("Restart required")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(applyMutateAsync).toHaveBeenCalledTimes(1));
  });

  it("disables Apply while there are unsaved edits", () => {
    render(<ConfigTab server={server} />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "dirty.example.com" } });
    expect(screen.getByRole("button", { name: "Apply to node" })).toBeDisabled();
    expect(screen.getByText("Save before applying")).toBeInTheDocument();
  });
});
