import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { GroupConfig } from "@/shared/api/schemas/config";

// useUnsavedChangesGuard (added by audit E4) calls useBlocker + useConfirm.
// Mock both so the section can be tested without a Router or ConfirmProvider.
const navigateSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useBlocker: vi.fn(),
  useNavigate: () => navigateSpy,
}));

vi.mock("@/features/servers/hooks/useServersList", () => ({
  useServersList: () => ({ servers: [], agentVersions: {}, isLoading: false, error: null }),
}));
vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => vi.fn().mockResolvedValue(true),
}));

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
// Async group apply: the mutation resolves with the 202 batch (batch id +
// per-agent job handles), and the polling hook reports aggregate/per-agent
// status. Tests override applyStatus to simulate in-flight / done rollouts.
const applyMutateAsync = vi.fn().mockResolvedValue({
  batch_id: "cfgapply-1",
  jobs: [
    { agent_id: "agent-1", job_id: "job-1" },
    { agent_id: "agent-2", job_id: "job-2" },
  ],
});
const useGroupConfig = vi.fn();
const useGroupConfigApplyStatus = vi.fn();
vi.mock("@/features/servers/config/configHooks", () => ({
  useGroupConfig: (id: string) => useGroupConfig(id),
  usePutGroupConfig: () => ({ mutate: putMutate, isPending: false }),
  useApplyGroupConfig: () => ({ mutateAsync: applyMutateAsync, isPending: false }),
  useGroupConfigApplyStatus: (
    groupId: string,
    batchId: string | null,
    handles: unknown,
  ) => useGroupConfigApplyStatus(groupId, batchId, handles),
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
    // Default: no rollout in flight (no batch kicked off yet).
    useGroupConfigApplyStatus.mockReturnValue({
      data: undefined,
      isFetching: false,
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

  // 3.12: a background refetch (e.g. the async-apply status poll, or the WS
  // seq-gap full-cache invalidation) firing while the operator is mid-edit
  // must not wipe their unsaved draft.
  it("does NOT clobber unsaved edits when the query data is refetched (same group)", () => {
    const { rerender } = render(<GroupConfigSection groupId="fg-1" />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "dirty.example.com" } });
    expect(screen.getByDisplayValue("dirty.example.com")).toBeInTheDocument();

    // Simulate a refetch that returns a NEW object with the SAME logical
    // target sections (identity changes on every query settle, even when
    // the server-side value hasn't changed).
    useGroupConfig.mockReturnValue({
      data: makeConfig({ sections: { censorship: { tls_domain: "old.example.com" } } }),
      isLoading: false,
      isError: false,
    });
    rerender(<GroupConfigSection groupId="fg-1" />);

    expect(screen.getByDisplayValue("dirty.example.com")).toBeInTheDocument();
  });

  it("re-seeds the editor when the group id changes, even mid-edit", () => {
    const { rerender } = render(<GroupConfigSection groupId="fg-1" />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "dirty.example.com" } });

    useGroupConfig.mockReturnValue({
      data: makeConfig({ sections: { censorship: { tls_domain: "fresh.example.com" } } }),
      isLoading: false,
      isError: false,
    });
    rerender(<GroupConfigSection groupId="fg-2" />);

    expect(screen.getByDisplayValue("fresh.example.com")).toBeInTheDocument();
  });

  it("re-seeds the editor on refetch when the draft is NOT dirty", () => {
    const { rerender } = render(<GroupConfigSection groupId="fg-1" />);
    expect(screen.getByDisplayValue("old.example.com")).toBeInTheDocument();

    useGroupConfig.mockReturnValue({
      data: makeConfig({ sections: { censorship: { tls_domain: "server-updated.example.com" } } }),
      isLoading: false,
      isError: false,
    });
    rerender(<GroupConfigSection groupId="fg-1" />);

    expect(screen.getByDisplayValue("server-updated.example.com")).toBeInTheDocument();
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

  it("renders per-agent rollout progress while the async apply is in flight", () => {
    // Simulate the poller mid-rollout: one done, one still applying.
    useGroupConfigApplyStatus.mockReturnValue({
      data: {
        done: false,
        total: 2,
        applied: 1,
        failed: 0,
        pending: 1,
        agents: [
          { agent_id: "agent-1", job_id: "job-1", status: "succeeded", message: "" },
          { agent_id: "agent-2", job_id: "job-2", status: "running", message: "" },
        ],
      },
      isFetching: true,
    });
    render(<GroupConfigSection groupId="fg-1" />);
    expect(screen.getByText("Rollout progress")).toBeInTheDocument();
    // Per-agent status pills surface each node's state.
    expect(screen.getByText("Done")).toBeInTheDocument();
    expect(screen.getByText("Applying")).toBeInTheDocument();
    // In-flight rollout does NOT toast a terminal result yet.
    expect(toastApi.success).not.toHaveBeenCalled();
    expect(toastApi.error).not.toHaveBeenCalled();
  });

  it("surfaces a PARTIAL failure via toast + a failed pill when the rollout ends mixed", () => {
    useGroupConfigApplyStatus.mockReturnValue({
      data: {
        done: true,
        total: 2,
        applied: 1,
        failed: 1,
        pending: 0,
        agents: [
          { agent_id: "agent-1", job_id: "job-1", status: "succeeded", message: "" },
          {
            agent_id: "agent-2",
            job_id: "job-2",
            status: "failed",
            message: "health check failed",
          },
        ],
      },
      isFetching: false,
    });
    render(<GroupConfigSection groupId="fg-1" />);
    // The failing agent gets a Failed pill — the partial outcome is visible.
    expect(screen.getByText("Failed")).toBeInTheDocument();
    // And the terminal partial toast fires exactly once.
    expect(toastApi.error).toHaveBeenCalledWith(
      "Applied to 1 of 2 node(s), 1 failed",
    );
  });

  it("toasts a clean success when every agent succeeds", () => {
    useGroupConfigApplyStatus.mockReturnValue({
      data: {
        done: true,
        total: 2,
        applied: 2,
        failed: 0,
        pending: 0,
        agents: [
          { agent_id: "agent-1", job_id: "job-1", status: "succeeded", message: "" },
          { agent_id: "agent-2", job_id: "job-2", status: "succeeded", message: "" },
        ],
      },
      isFetching: false,
    });
    render(<GroupConfigSection groupId="fg-1" />);
    expect(toastApi.success).toHaveBeenCalledWith("Applied to all 2 node(s)");
  });
});
