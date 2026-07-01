import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Mock the config API surface the hooks call.
vi.mock("@/shared/api/config", () => ({
  configApi: {
    groupConfigApplyStatus: vi.fn(),
    getGroupConfigApplyBatch: vi.fn(),
    activeGroupConfigApplyBatch: vi.fn(),
  },
}));

// The polling hook invalidates the group config query on each refetch; a
// thin toast fake keeps the module graph satisfied.
vi.mock("@/app/providers/ToastProvider", () => ({
  useToast: () => ({
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    withAction: vi.fn(),
    dismiss: vi.fn(),
  }),
}));

import { configApi } from "@/shared/api/config";
import {
  useActiveGroupConfigApplyBatch,
  useGroupConfigApplyBatch,
  useGroupConfigApplyStatus,
} from "./configHooks";
import type {
  GroupApplyBatchStatus,
  GroupApplyStatus,
} from "@/shared/api/schemas/config";

const statusMock = vi.mocked(configApi.groupConfigApplyStatus);
const batchStatusMock = vi.mocked(configApi.getGroupConfigApplyBatch);
const activeBatchMock = vi.mocked(configApi.activeGroupConfigApplyBatch);

function wrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

const doneStatus: GroupApplyStatus = {
  done: true,
  total: 2,
  applied: 1,
  failed: 1,
  pending: 0,
  agents: [
    { agent_id: "a1", job_id: "job-1", status: "succeeded", message: "" },
    { agent_id: "a2", job_id: "job-2", status: "failed", message: "boom" },
  ],
};

describe("useGroupConfigApplyStatus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("is disabled (no fetch) when there is no active batch", () => {
    renderHook(
      () => useGroupConfigApplyStatus("fg-1", null, []),
      { wrapper: wrapper() },
    );
    expect(statusMock).not.toHaveBeenCalled();
  });

  it("is disabled when every job handle is a no-op (empty job id)", () => {
    renderHook(
      () =>
        useGroupConfigApplyStatus("fg-1", "batch-1", [
          { agent_id: "a1", job_id: "" },
        ]),
      { wrapper: wrapper() },
    );
    expect(statusMock).not.toHaveBeenCalled();
  });

  it("polls the status endpoint and reports the aggregate once a batch is active", async () => {
    statusMock.mockResolvedValue(doneStatus);
    const { result } = renderHook(
      () =>
        useGroupConfigApplyStatus("fg-1", "batch-1", [
          { agent_id: "a1", job_id: "job-1" },
          { agent_id: "a2", job_id: "job-2" },
        ]),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.data?.done).toBe(true));
    expect(statusMock).toHaveBeenCalledWith("fg-1", [
      { agent_id: "a1", job_id: "job-1" },
      { agent_id: "a2", job_id: "job-2" },
    ]);
    // Partial failure is represented in the aggregate the UI renders.
    expect(result.current.data?.applied).toBe(1);
    expect(result.current.data?.failed).toBe(1);
  });
});

const doneBatchStatus: GroupApplyBatchStatus = {
  batch_id: "batch-1",
  mode: "all_at_once",
  status: "failed",
  done: true,
  total: 2,
  applied: 1,
  failed: 1,
  pending: 0,
  skipped: 0,
  agents: [
    { agent_id: "a1", job_id: "job-1", status: "succeeded", message: "" },
    { agent_id: "a2", job_id: "job-2", status: "failed", message: "boom" },
  ],
};

describe("useGroupConfigApplyBatch", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("is disabled (no fetch) when there is no batch id", () => {
    renderHook(() => useGroupConfigApplyBatch("fg-1", null), { wrapper: wrapper() });
    expect(batchStatusMock).not.toHaveBeenCalled();
  });

  it("polls the persistent batch-status endpoint until the batch is done", async () => {
    batchStatusMock.mockResolvedValue(doneBatchStatus);
    const { result } = renderHook(
      () => useGroupConfigApplyBatch("fg-1", "batch-1"),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.data?.done).toBe(true));
    expect(batchStatusMock).toHaveBeenCalledWith("fg-1", "batch-1");
    expect(result.current.data?.status).toBe("failed");
    expect(result.current.data?.agents[1]?.message).toBe("boom");
  });
});

describe("useActiveGroupConfigApplyBatch", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("is disabled (no fetch) without a group id", () => {
    renderHook(() => useActiveGroupConfigApplyBatch(""), { wrapper: wrapper() });
    expect(activeBatchMock).not.toHaveBeenCalled();
  });

  it("resolves undefined when the group has no batch in flight", async () => {
    activeBatchMock.mockResolvedValue(undefined);
    const { result } = renderHook(
      () => useActiveGroupConfigApplyBatch("fg-1"),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(activeBatchMock).toHaveBeenCalledWith("fg-1");
    expect(result.current.data).toBeNull();
  });

  it("resolves the running batch id when one is active", async () => {
    activeBatchMock.mockResolvedValue({ batch_id: "batch-resume" });
    const { result } = renderHook(
      () => useActiveGroupConfigApplyBatch("fg-1"),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.data?.batch_id).toBe("batch-resume"));
  });
});
