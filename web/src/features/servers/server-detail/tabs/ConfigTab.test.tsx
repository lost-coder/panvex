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
const applyMutateAsync = vi.fn().mockResolvedValue({ batch_id: "batch-1" });
const useAgentConfig = vi.fn();
const useAgentConfigApplyBatch = vi.fn();
vi.mock("@/features/servers/config/configHooks", () => ({
  useAgentConfig: (id: string) => useAgentConfig(id),
  useAgentConfigApplyBatch: (agentId: string, batchId: string | null) =>
    useAgentConfigApplyBatch(agentId, batchId),
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
    // No apply batch in flight by default; individual tests override this to
    // simulate a terminal batch and assert the completion toast.
    useAgentConfigApplyBatch.mockReturnValue({ data: undefined });
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

  it("shows the in-progress indicator on Apply and toasts success when the batch completes", async () => {
    // Hot-only override so Apply kicks off directly (no restart dialog).
    useAgentConfig.mockReturnValue({
      data: makeConfig({
        override: { general: { log_level: "info" } },
        effective: { general: { log_level: "info" } },
        drift: { status: "in_sync", fields: [] },
      }),
      isLoading: false,
      isError: false,
    });
    const { rerender } = render(<ConfigTab server={server} />);
    fireEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    await waitFor(() => expect(applyMutateAsync).toHaveBeenCalledTimes(1));
    // Live "applying…" indicator shows while the batch is in flight.
    await screen.findByText("Applying configuration…");

    // The batch poll settles done with a clean result → success toast fires
    // here (the button is kickoff-only).
    useAgentConfigApplyBatch.mockReturnValue({
      data: {
        batch_id: "batch-1",
        mode: "all_at_once",
        status: "succeeded",
        done: true,
        total: 1,
        applied: 1,
        failed: 0,
        pending: 0,
        skipped: 0,
        agents: [
          { agent_id: "agent-7", job_id: "job-1", status: "succeeded", message: "" },
        ],
      },
    });
    rerender(<ConfigTab server={server} />);
    await waitFor(() =>
      expect(toastApi.success).toHaveBeenCalledWith("Applied to 1 node(s)"),
    );
  });

  it("disables Apply while there are unsaved edits", () => {
    render(<ConfigTab server={server} />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "dirty.example.com" } });
    expect(screen.getByRole("button", { name: "Apply to node" })).toBeDisabled();
    expect(screen.getByText("Save before applying")).toBeInTheDocument();
  });

  // 3.12: a background refetch (e.g. the WS seq-gap full-cache
  // invalidation) firing while the operator is mid-edit must not wipe
  // their unsaved draft.
  it("does NOT clobber unsaved edits when the query data is refetched (same server)", () => {
    const { rerender } = render(<ConfigTab server={server} />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "dirty.example.com" } });
    expect(screen.getByDisplayValue("dirty.example.com")).toBeInTheDocument();

    // Simulate a refetch that returns a NEW object with the SAME logical
    // override (identity changes on every query settle, even when the
    // server-side value hasn't changed).
    useAgentConfig.mockReturnValue({
      data: makeConfig({ override: { censorship: { tls_domain: "old.example.com" } } }),
      isLoading: false,
      isError: false,
    });
    rerender(<ConfigTab server={server} />);

    expect(screen.getByDisplayValue("dirty.example.com")).toBeInTheDocument();
  });

  it("re-seeds the editor when the server id changes, even mid-edit", () => {
    const { rerender } = render(<ConfigTab server={server} />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "dirty.example.com" } });

    const otherServer = { id: "agent-99", name: "edge-2" } as ServerDetailPageProps["server"];
    useAgentConfig.mockReturnValue({
      data: makeConfig({ override: { censorship: { tls_domain: "fresh.example.com" } } }),
      isLoading: false,
      isError: false,
    });
    rerender(<ConfigTab server={otherServer} />);

    expect(screen.getByDisplayValue("fresh.example.com")).toBeInTheDocument();
  });

  it("re-seeds the editor on refetch when the draft is NOT dirty", () => {
    const { rerender } = render(<ConfigTab server={server} />);
    expect(screen.getByDisplayValue("old.example.com")).toBeInTheDocument();

    useAgentConfig.mockReturnValue({
      data: makeConfig({ override: { censorship: { tls_domain: "server-updated.example.com" } } }),
      isLoading: false,
      isError: false,
    });
    rerender(<ConfigTab server={server} />);

    expect(screen.getByDisplayValue("server-updated.example.com")).toBeInTheDocument();
  });
});
