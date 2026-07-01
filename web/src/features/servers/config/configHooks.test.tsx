import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Mock the config API surface the hooks call.
vi.mock("@/shared/api/config", () => ({
  configApi: {
    groupConfigApplyStatus: vi.fn(),
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
import { useGroupConfigApplyStatus } from "./configHooks";
import type { GroupApplyStatus } from "@/shared/api/schemas/config";

const statusMock = vi.mocked(configApi.groupConfigApplyStatus);

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
