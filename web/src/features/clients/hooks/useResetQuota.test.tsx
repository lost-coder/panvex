// 7.6 (аудит, LOW): queryFn у poll-query обязан быть чистым — side-effect
// (processJobs: setState/тосты/резолверы) переезжает в useEffect по
// query.data. Тест ловит регресс через подсчёт тостов при двойном
// прогоне и проверяет, что unmount сеттлит висящие резолверы.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/api", () => ({
  apiClient: {
    jobs: vi.fn(),
    resetClientQuotaOnAgent: vi.fn(),
    resetClientQuotaFanOut: vi.fn(),
  },
}));

import { apiClient } from "@/shared/api/api";
import { useResetQuota } from "./useResetQuota";

const CLIENT_ID = "c1";
const AGENT_ID = "agent-1";
const JOB_ID = "job-1";

function wrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

function terminalJobs() {
  return [
    {
      id: JOB_ID,
      targets: [
        {
          agent_id: AGENT_ID,
          status: "succeeded",
          result_json: "{}",
          result_text: "",
        },
      ],
    },
  ];
}

describe("useResetQuota (7.6)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("resolves resetOnAgent and toasts exactly once when the job lands terminal", async () => {
    (apiClient.resetClientQuotaOnAgent as ReturnType<typeof vi.fn>).mockResolvedValue({
      job: { id: JOB_ID, targets: [] },
    });
    (apiClient.jobs as ReturnType<typeof vi.fn>).mockResolvedValue(terminalJobs());
    const onToast = vi.fn();

    const { result } = renderHook(() => useResetQuota(CLIENT_ID, onToast), {
      wrapper: wrapper(),
    });

    let outcomePromise: Promise<unknown> | null = null;
    await act(async () => {
      outcomePromise = result.current.resetOnAgent(AGENT_ID);
    });

    await waitFor(() => {
      expect(result.current.rowStates[AGENT_ID]).toEqual({ kind: "success" });
    });
    await expect(outcomePromise).resolves.toEqual({ kind: "success" });
    expect(onToast).toHaveBeenCalledTimes(1);
    expect(onToast).toHaveBeenCalledWith("agent", { agentId: AGENT_ID });
  });

  it("settles pending resolvers on unmount (no dangling promises)", async () => {
    (apiClient.resetClientQuotaOnAgent as ReturnType<typeof vi.fn>).mockResolvedValue({
      job: { id: JOB_ID, targets: [] },
    });
    // Джоба никогда не становится терминальной — резолвер висит до анмаунта.
    (apiClient.jobs as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    const onToast = vi.fn();

    const { result, unmount } = renderHook(() => useResetQuota(CLIENT_ID, onToast), {
      wrapper: wrapper(),
    });

    let outcomePromise: Promise<unknown> | null = null;
    await act(async () => {
      outcomePromise = result.current.resetOnAgent(AGENT_ID);
    });

    unmount();

    await expect(outcomePromise).resolves.toEqual({
      kind: "failed",
      error: "cancelled",
    });
    expect(onToast).not.toHaveBeenCalled();
  });
});
