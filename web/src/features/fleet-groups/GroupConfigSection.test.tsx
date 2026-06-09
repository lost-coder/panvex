import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { GroupConfig } from "@/shared/api/schemas/config";

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

// Mock the config hooks so the section can be exercised without a
// QueryClient or network — the same isolation strategy ConfigTab.test uses.
const putMutate = vi.fn();
const applyMutateAsync = vi.fn().mockResolvedValue({ applied: 2, failed: "", error: "" });
const useGroupConfig = vi.fn();
vi.mock("@/features/servers/config/configHooks", () => ({
  useGroupConfig: (id: string) => useGroupConfig(id),
  usePutGroupConfig: () => ({ mutate: putMutate, isPending: false }),
  useApplyGroupConfig: () => ({ mutateAsync: applyMutateAsync, isPending: false }),
}));

import { GroupConfigSection } from "./GroupConfigSection";

function makeConfig(overrides: Partial<GroupConfig> = {}): GroupConfig {
  return {
    sections: { censorship: { tls_domain: "old.example.com" } },
    nodes: [
      { agent_id: "agent-1", status: "in_sync" },
      { agent_id: "agent-2", status: "drifted" },
    ],
    ...overrides,
  };
}

describe("GroupConfigSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useGroupConfig.mockReturnValue({
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

  it("seeds the editor from the group target sections", () => {
    render(<GroupConfigSection groupId="fg-1" />);
    expect(screen.getByDisplayValue("old.example.com")).toBeInTheDocument();
  });

  it("saves the unflattened sections when Save is clicked", () => {
    render(<GroupConfigSection groupId="fg-1" />);
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
    render(<GroupConfigSection groupId="fg-1" />);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(toastApi.success).toHaveBeenCalledWith("Configuration saved");
  });

  it("renders a drift badge per node with the right status", () => {
    render(<GroupConfigSection groupId="fg-1" />);
    // Node ids are listed.
    expect(screen.getByText("agent-1")).toBeInTheDocument();
    expect(screen.getByText("agent-2")).toBeInTheDocument();
    // One in-sync, one drifted badge.
    expect(screen.getByText("In sync")).toBeInTheDocument();
    expect(screen.getByText("Drifted")).toBeInTheDocument();
  });

  it("renders the group Apply button", () => {
    render(<GroupConfigSection groupId="fg-1" />);
    expect(screen.getByRole("button", { name: "Apply to group" })).toBeInTheDocument();
  });

  it("rolls out the saved target on confirm (restart field → warning dialog)", async () => {
    render(<GroupConfigSection groupId="fg-1" />);
    // The target holds a restart-only field (censorship.tls_domain), so Apply
    // opens the restart-warning confirm before rolling out.
    fireEvent.click(screen.getByRole("button", { name: "Apply to group" }));
    expect(screen.getByText("Restart required")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(applyMutateAsync).toHaveBeenCalledTimes(1));
  });

  it("disables Apply while there are unsaved edits", () => {
    render(<GroupConfigSection groupId="fg-1" />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "dirty.example.com" } });
    expect(screen.getByRole("button", { name: "Apply to group" })).toBeDisabled();
    expect(screen.getByText("Save before applying")).toBeInTheDocument();
  });

  it("shows an empty-state message when the group has no nodes", () => {
    useGroupConfig.mockReturnValue({
      data: makeConfig({ nodes: [] }),
      isLoading: false,
      isError: false,
    });
    render(<GroupConfigSection groupId="fg-1" />);
    expect(screen.getByText("No nodes in this group yet")).toBeInTheDocument();
  });

  it("falls back to the unknown drift status for unrecognised node states", () => {
    useGroupConfig.mockReturnValue({
      data: makeConfig({ nodes: [{ agent_id: "agent-9", status: "bogus" }] }),
      isLoading: false,
      isError: false,
    });
    render(<GroupConfigSection groupId="fg-1" />);
    expect(screen.getByText("Unknown")).toBeInTheDocument();
  });

  it("shows a loading state while the query is pending", () => {
    useGroupConfig.mockReturnValue({ data: undefined, isLoading: true, isError: false });
    render(<GroupConfigSection groupId="fg-1" />);
    expect(screen.getByText("Loading…")).toBeInTheDocument();
  });

  it("shows an error state when the query fails", () => {
    useGroupConfig.mockReturnValue({ data: undefined, isLoading: false, isError: true });
    render(<GroupConfigSection groupId="fg-1" />);
    expect(screen.getByText("Request failed")).toBeInTheDocument();
  });
});
