// Task 14: the 409-guard-code → localized-toast mapping lives inside
// useDispatchTelemtUpdate itself (TelemtUpdateSection.test.tsx mocks this
// hook away entirely, so the mapping needs its own coverage here).
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/api", () => {
  class ApiError extends Error {
    code?: string | undefined;
    status?: number | undefined;
    constructor(message: string, code?: string, status?: number) {
      super(message);
      this.code = code;
      this.status = status;
    }
  }
  return {
    ApiError,
    apiClient: { dispatchTelemtUpdate: vi.fn() },
  };
});

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

import { ApiError, apiClient } from "@/shared/api/api";
import { useDispatchTelemtUpdate } from "./useTelemtUpdateStrategy";

const dispatchMock = vi.mocked(apiClient.dispatchTelemtUpdate);

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

describe("useDispatchTelemtUpdate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("toasts a success message when the dispatch is accepted", async () => {
    dispatchMock.mockResolvedValue({ job_id: "job-1" });
    const { result } = renderHook(() => useDispatchTelemtUpdate("agent-1"), { wrapper: wrapper() });

    result.current.mutate({ version: "3.4.25" });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(dispatchMock).toHaveBeenCalledWith("agent-1", { version: "3.4.25" });
    expect(toastApi.success).toHaveBeenCalledWith("Telemt update queued");
  });

  it.each([
    ["strategy_not_configured", "No Telemt update strategy is configured for this node"],
    ["mode_not_binary", "The configured strategy does not support an in-place binary update"],
    ["update_unavailable", "The agent reports an in-place update is not available right now"],
    ["no_known_release", "No known Telemt release yet — check for updates first or specify a version"],
  ])("maps the %s 409 code to a localized toast", async (code, expected) => {
    dispatchMock.mockRejectedValue(new ApiError("conflict", code, 409));
    const { result } = renderHook(() => useDispatchTelemtUpdate("agent-1"), { wrapper: wrapper() });

    result.current.mutate({ version: "3.4.25" });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(toastApi.error).toHaveBeenCalledWith(expected);
  });

  it("falls back to the raw server message for an unrecognised error code", async () => {
    dispatchMock.mockRejectedValue(new ApiError("agent not found", "not_found", 404));
    const { result } = renderHook(() => useDispatchTelemtUpdate("agent-1"), { wrapper: wrapper() });

    result.current.mutate({ version: "3.4.25" });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(toastApi.error).toHaveBeenCalledWith("agent not found");
  });
});
