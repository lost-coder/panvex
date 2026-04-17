import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import * as React from "react";
import { describe, expect, it, vi } from "vitest";

// Mock api.ts so the hook never touches real fetch; we hand it a canned
// payload shaped like the server response and assert the transform +
// wrapping.
vi.mock("@/lib/api", () => ({
  apiClient: {
    clients: vi.fn(),
  },
}));

import { apiClient } from "@/lib/api";
import { useClientsList } from "./useClientsList";

function wrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

describe("useClientsList", () => {
  it("returns [] before the fetch resolves", () => {
    (apiClient.clients as ReturnType<typeof vi.fn>).mockReturnValue(
      new Promise(() => {}), // never resolves
    );

    const { result } = renderHook(() => useClientsList(), {
      wrapper: wrapper(),
    });

    expect(result.current.clients).toEqual([]);
    expect(result.current.isLoading).toBe(true);
  });

  it("transforms the API payload into UI ClientListItems", async () => {
    (apiClient.clients as ReturnType<typeof vi.fn>).mockResolvedValueOnce([
      {
        id: "c1",
        name: "alpha",
        enabled: true,
        assigned_nodes_count: 2,
        expiration_rfc3339: "2030-01-01T00:00:00Z",
        traffic_used_bytes: 1024,
        unique_ips_used: 3,
        active_tcp_conns: 7,
        data_quota_bytes: 10_000,
        last_deploy_status: "applied",
      },
    ]);

    const { result } = renderHook(() => useClientsList(), {
      wrapper: wrapper(),
    });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(result.current.clients).toHaveLength(1);
    expect(result.current.clients[0]).toMatchObject({
      id: "c1",
      name: "alpha",
      assignedNodesCount: 2,
      trafficUsedBytes: 1024,
      activeTcpConns: 7,
    });
    expect(result.current.error).toBeFalsy();
  });

  it("surfaces fetch failure as query error", async () => {
    (apiClient.clients as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error("network down"),
    );

    const { result } = renderHook(() => useClientsList(), {
      wrapper: wrapper(),
    });

    await waitFor(() => {
      expect(result.current.error).toBeTruthy();
    });

    expect((result.current.error as Error).message).toBe("network down");
    expect(result.current.clients).toEqual([]);
  });
});
