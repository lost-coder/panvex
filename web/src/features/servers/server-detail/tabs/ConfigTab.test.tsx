import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
    desired: { censorship: { tls_domain: "old.example.com" } },
    effective: { censorship: { tls_domain: "old.example.com" } },
    observed: {},
    drift: { status: "drifted", fields: ["censorship.tls_domain"] },
    group_paths: [],
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

  it("seeds the tree from desired", () => {
    render(<ConfigTab server={server} />);
    expect(screen.getByDisplayValue("old.example.com")).toBeInTheDocument();
  });

  // P4-T5: the GET response's group_paths must reach ConfigTree so it can
  // lock the fields the node's fleet group governs — asserts the wiring
  // (ConfigTab -> ConfigTree -> ConfigTreeField), not the locking logic
  // itself (already covered by ConfigTree/ConfigTreeField's own tests).
  it("locks a field whose path is present in group_paths", () => {
    useAgentConfig.mockReturnValue({
      data: makeConfig({ group_paths: ["censorship.tls_domain"] }),
      isLoading: false,
      isError: false,
    });
    render(<ConfigTab server={server} />);
    expect(screen.getByText(/set by group|управляется группой/i)).toBeInTheDocument();
    expect(screen.getByDisplayValue("old.example.com")).toBeDisabled();
  });

  // On an install with no group config and no override the panel knows the
  // node's real settings only through `observed`. buildTree falls back to
  // the observed value for any path with no desired entry, so the field
  // renders pre-filled with what the node actually runs rather than blank.
  it("falls back to the observed value for fields with no desired override", () => {
    useAgentConfig.mockReturnValue({
      data: makeConfig({
        desired: {},
        effective: {},
        observed: {
          timeouts: { client_handshake: 12 },
        },
      }),
      isLoading: false,
      isError: false,
    });
    render(<ConfigTab server={server} />);
    expect(screen.getByDisplayValue("12")).toBeInTheDocument();
  });

  it("renders the config tree seeded from desired and shows drift actions", () => {
    useAgentConfig.mockReturnValue({
      data: {
        desired: { general: { log_level: "normal" } },
        effective: { general: { log_level: "normal" } },
        observed: { general: { log_level: "silent" } },
        drift: { status: "drifted", fields: ["general.log_level"] },
      },
      isLoading: false,
      isError: false,
    });
    render(<ConfigTab server={server} />);
    expect(screen.getByText("log_level")).toBeInTheDocument();
    expect(screen.getByText(/on node: silent|на ноде: silent/i)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /accept node|принять значение ноды/i }),
    ).toBeInTheDocument();
  });

  it("Accept node value PUTs just the observed value at that path", () => {
    useAgentConfig.mockReturnValue({
      data: makeConfig({
        desired: { general: { log_level: "normal" } },
        effective: { general: { log_level: "normal" } },
        observed: { general: { log_level: "silent" } },
        drift: { status: "drifted", fields: ["general.log_level"] },
      }),
      isLoading: false,
      isError: false,
    });
    render(<ConfigTab server={server} />);
    fireEvent.click(screen.getByRole("button", { name: "Accept node value" }));
    expect(putMutate).toHaveBeenCalledTimes(1);
    expect(putMutate.mock.calls[0]?.[0]).toEqual({ general: { log_level: "silent" } });
  });

  // Regression: handleAcceptNode used to PUT the accepted path server-side
  // but never touch local `values`/`lastSeededRef`. If another path was
  // dirty at the same time, the reseed-from-refetch effect bails (it only
  // resyncs when nothing is dirty), so the accepted path kept its stale
  // local value — and a later whole-map Save silently reverted the just
  // accepted server value back to the old one.
  it("Accept node value survives a later Save even when another field is mid-edit", () => {
    putMutate.mockImplementation((_body: unknown, opts?: { onSuccess?: () => void }) =>
      opts?.onSuccess?.(),
    );
    useAgentConfig.mockReturnValue({
      data: makeConfig({
        desired: {
          censorship: { tls_domain: "old.example.com" },
          general: { log_level: "normal" },
        },
        effective: {
          censorship: { tls_domain: "old.example.com" },
          general: { log_level: "normal" },
        },
        observed: { general: { log_level: "silent" } },
        drift: { status: "drifted", fields: ["general.log_level"] },
      }),
      isLoading: false,
      isError: false,
    });
    render(<ConfigTab server={server} />);

    // Dirty an unrelated field FIRST — this is what previously blocked the
    // reseed effect from ever picking up the accepted value.
    fireEvent.change(screen.getByDisplayValue("old.example.com"), {
      target: { value: "dirty.example.com" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Accept node value" }));
    expect(putMutate).toHaveBeenNthCalledWith(
      1,
      { general: { log_level: "silent" } },
      expect.anything(),
    );

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(putMutate).toHaveBeenCalledTimes(2);
    // The accepted value (silent) must survive into the Save payload
    // alongside the still-dirty tls_domain edit — NOT the old desired
    // value (normal) the stale `values` map used to carry. Plus the
    // (empty, since this fixture has none) containers buildSections always
    // writes alongside the scalars.
    expect(putMutate.mock.calls[1]?.[0]).toEqual({
      censorship: { tls_domain: "dirty.example.com", exclusive_mask: {} },
      general: { log_level: "silent" },
      dc_overrides: {},
      upstreams: [],
    });
  });

  it("Revert to panel value applies just that one path", async () => {
    useAgentConfig.mockReturnValue({
      data: makeConfig({
        desired: { general: { log_level: "normal" } },
        effective: { general: { log_level: "normal" } },
        observed: { general: { log_level: "silent" } },
        drift: { status: "drifted", fields: ["general.log_level"] },
      }),
      isLoading: false,
      isError: false,
    });
    render(<ConfigTab server={server} />);
    fireEvent.click(screen.getByRole("button", { name: "Revert to panel value" }));
    await waitFor(() =>
      expect(applyMutateAsync).toHaveBeenCalledWith({ paths: ["general.log_level"] }),
    );
  });

  it("saves the unflattened sections when Save is clicked", () => {
    render(<ConfigTab server={server} />);
    const input = screen.getByDisplayValue("old.example.com");
    fireEvent.change(input, { target: { value: "new.example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(putMutate).toHaveBeenCalledTimes(1);
    // Body is the nested-sections shape produced by unflattenPaths, plus the
    // (empty, since this fixture has none) containers buildSections always
    // writes alongside the scalars.
    expect(putMutate.mock.calls[0]?.[0]).toEqual({
      censorship: { tls_domain: "new.example.com", exclusive_mask: {} },
      dc_overrides: {},
      upstreams: [],
    });
  });

  // Task 6: the Save payload must carry both the flat scalar overrides (via
  // dotted-path unflatten) AND the structural containers the two new
  // editors (UpstreamsEditor / MapEditor) manage — omitting an unchanged
  // container would leave a stale one behind, since the server MERGES the
  // PUT into the stored snapshot rather than replacing it.
  it("PUT несёт и скаляры, и контейнеры во вложенной форме", () => {
    useAgentConfig.mockReturnValue({
      data: {
        desired: {
          general: { log_level: "silent", links: { public_host: "ds87j.metrion.click" } },
          upstreams: [{ type: "direct", weight: 1 }],
          dc_overrides: { "203": ["91.105.192.100:443"] },
          censorship: { exclusive_mask: { "hv24s.metrion.icu": "127.0.0.1:8085" } },
        },
        effective: {},
        observed: {},
        drift: { status: "in_sync", fields: [] },
        group_paths: [],
      },
      isLoading: false,
      isError: false,
    });

    render(<ConfigTab server={server} />);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const body = putMutate.mock.calls.at(-1)?.[0] as Record<string, unknown>;
    // Nested table, not a flat "links.public_host" key.
    expect(body.general).toMatchObject({ links: { public_host: "ds87j.metrion.click" } });
    // Containers are not lost on Save.
    expect(body.upstreams).toEqual([{ type: "direct", weight: 1 }]);
    expect(body.dc_overrides).toEqual({ "203": ["91.105.192.100:443"] });
    expect(body.censorship).toMatchObject({
      exclusive_mask: { "hv24s.metrion.icu": "127.0.0.1:8085" },
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

  it("applies the persisted override on confirm (restart field + drift → combined warning dialog)", async () => {
    render(<ConfigTab server={server} />);
    // The override holds a reload-only field (censorship.tls_domain) AND
    // the fixture's drift status is "drifted" on that same field, so Apply
    // must show a SINGLE dialog carrying both the drift warning and the
    // session-policy choice — not two dialogs in sequence.
    fireEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    const dialogs = screen.getAllByRole("dialog");
    expect(dialogs).toHaveLength(1);
    const dialog = within(dialogs[0]!);
    expect(dialog.getByText("Node has drifted")).toBeInTheDocument();
    expect(dialog.getByText(/censorship\.tls_domain/)).toBeInTheDocument();
    expect(dialog.getByRole("radio", { name: /drain/i })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(applyMutateAsync).toHaveBeenCalledTimes(1));
  });

  it("shows only the session-policy choice when Apply is not drifted", async () => {
    useAgentConfig.mockReturnValue({
      data: makeConfig({ drift: { status: "in_sync", fields: [] } }),
      isLoading: false,
      isError: false,
    });
    render(<ConfigTab server={server} />);
    fireEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    expect(screen.getByRole("heading", { name: "Reload required" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(applyMutateAsync).toHaveBeenCalledTimes(1));
  });

  it("shows the in-progress indicator on Apply and toasts success when the batch completes", async () => {
    // Hot-only override so Apply kicks off directly (no restart dialog).
    useAgentConfig.mockReturnValue({
      data: makeConfig({
        desired: { general: { log_level: "info" } },
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
      data: makeConfig({ desired: { censorship: { tls_domain: "old.example.com" } } }),
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
      data: makeConfig({ desired: { censorship: { tls_domain: "fresh.example.com" } } }),
      isLoading: false,
      isError: false,
    });
    rerender(<ConfigTab server={otherServer} />);

    expect(screen.getByDisplayValue("fresh.example.com")).toBeInTheDocument();
  });

  // Task 6 finding: MapEditor keeps a per-row text draft keyed by ROW INDEX
  // while the operator types (cleared on that row's blur, not on every
  // keystroke). A same-node background refetch is guarded against wiping a
  // dirty container edit (see the test above), but a server SWITCH forces
  // the containers to re-seed unconditionally, same as the scalar values —
  // and ConfigTab does not remount on a prop change alone. Without
  // `key={agentId}` on the MapEditor instances, the old node's live draft
  // would keep pointing at row index 0 after the switch, masking the new
  // node's value.
  it("does not leak a MapEditor draft across a server switch", () => {
    useAgentConfig.mockReturnValue({
      data: makeConfig({
        desired: { dc_overrides: { "203": "91.105.192.100:443" } },
      }),
      isLoading: false,
      isError: false,
    });
    const { rerender } = render(<ConfigTab server={server} />);

    const valueInput = screen.getByRole("textbox", {
      name: "Endpoints (ip:port, comma-separated) 1",
    });
    // Type without blurring — this is what creates a live per-row draft.
    fireEvent.change(valueInput, { target: { value: "91.105.192.100:443, " } });
    expect(valueInput).toHaveValue("91.105.192.100:443, ");

    const otherServer = { id: "agent-99", name: "edge-2" } as ServerDetailPageProps["server"];
    useAgentConfig.mockReturnValue({
      data: makeConfig({
        desired: { dc_overrides: { "301": "8.8.8.8:443" } },
      }),
      isLoading: false,
      isError: false,
    });
    rerender(<ConfigTab server={otherServer} />);

    // The new node's row must show ITS value, not the previous node's
    // in-flight draft text.
    const freshInput = screen.getByRole("textbox", {
      name: "Endpoints (ip:port, comma-separated) 1",
    });
    expect(freshInput).toHaveValue("8.8.8.8:443");
  });

  it("re-seeds the editor on refetch when the draft is NOT dirty", () => {
    const { rerender } = render(<ConfigTab server={server} />);
    expect(screen.getByDisplayValue("old.example.com")).toBeInTheDocument();

    useAgentConfig.mockReturnValue({
      data: makeConfig({ desired: { censorship: { tls_domain: "server-updated.example.com" } } }),
      isLoading: false,
      isError: false,
    });
    rerender(<ConfigTab server={server} />);

    expect(screen.getByDisplayValue("server-updated.example.com")).toBeInTheDocument();
  });
});
