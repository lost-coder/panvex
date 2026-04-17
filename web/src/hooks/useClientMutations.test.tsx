import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/api", () => ({
  apiClient: {
    updateClient: vi.fn(),
    rotateClientSecret: vi.fn(),
    deleteClient: vi.fn(),
  },
}));

// Build a thin fake toast so we can assert `toast.error(msg)` is the
// only side-effect the hook has on mutation failure.
const toastApi = {
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  dismiss: vi.fn(),
};
vi.mock("@/providers/ToastProvider", () => ({
  useToast: () => toastApi,
}));

import { apiClient } from "@/lib/api";
import { useClientMutations } from "./useClientMutations";
import type { Client as ApiClient, ClientInput } from "@/lib/api";

const rawClient: ApiClient = {
  id: "c1",
  name: "alpha",
  enabled: true,
  secret: "deadbeef",
  user_ad_tag: "",
  traffic_used_bytes: 0,
  unique_ips_used: 0,
  active_tcp_conns: 0,
  max_tcp_conns: 0,
  max_unique_ips: 0,
  data_quota_bytes: 0,
  expiration_rfc3339: "2030-01-01T00:00:00Z",
  fleet_group_ids: [],
  agent_ids: [],
  deployments: [],
  created_at_unix: 0,
  updated_at_unix: 0,
  deleted_at_unix: 0,
};

function wrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return {
    qc,
    Wrapper: ({ children }: { children: React.ReactNode }) =>
      React.createElement(QueryClientProvider, { client: qc }, children),
  };
}

describe("useClientMutations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("editMutation calls apiClient.updateClient and invalidates caches on success", async () => {
    (apiClient.updateClient as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...rawClient,
      name: "alpha-v2",
    });

    const { Wrapper, qc } = wrapper();
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(
      () => useClientMutations("c1", rawClient),
      { wrapper: Wrapper },
    );

    await result.current.editMutation.mutateAsync({
      name: "alpha-v2",
      userAdTag: "",
      maxTcpConns: 0,
      maxUniqueIps: 0,
      dataQuotaBytes: 0,
      expirationRfc3339: "2030-01-01T00:00:00Z",
    } as unknown as Parameters<typeof result.current.editMutation.mutateAsync>[0]);

    expect(apiClient.updateClient).toHaveBeenCalledTimes(1);
    const [clientId, payload] = (
      apiClient.updateClient as ReturnType<typeof vi.fn>
    ).mock.calls[0];
    expect(clientId).toBe("c1");
    expect((payload as ClientInput).name).toBe("alpha-v2");
    // Invalidates both the single-client key and the list key.
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["client", "c1"] });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["clients"] });
  });

  it("editMutation surfaces failures via toast.error", async () => {
    (apiClient.updateClient as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error("quota exceeded"),
    );

    const { Wrapper } = wrapper();
    const { result } = renderHook(
      () => useClientMutations("c1", rawClient),
      { wrapper: Wrapper },
    );

    await expect(
      result.current.editMutation.mutateAsync({
        name: "alpha-v2",
      } as unknown as Parameters<typeof result.current.editMutation.mutateAsync>[0]),
    ).rejects.toThrow("quota exceeded");

    await waitFor(() => {
      expect(toastApi.error).toHaveBeenCalledWith("quota exceeded");
    });
  });

  it("rotateMutation calls rotateClientSecret and invalidates single-client key", async () => {
    (apiClient.rotateClientSecret as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...rawClient,
      secret: "rotated",
    });

    const { Wrapper, qc } = wrapper();
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(
      () => useClientMutations("c1", rawClient),
      { wrapper: Wrapper },
    );

    await result.current.rotateMutation.mutateAsync();
    expect(apiClient.rotateClientSecret).toHaveBeenCalledWith("c1");
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["client", "c1"] });
  });

  it("deleteMutation calls deleteClient and invalidates list", async () => {
    (apiClient.deleteClient as ReturnType<typeof vi.fn>).mockResolvedValueOnce(undefined);

    const { Wrapper, qc } = wrapper();
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(
      () => useClientMutations("c1", rawClient),
      { wrapper: Wrapper },
    );

    await result.current.deleteMutation.mutateAsync();
    expect(apiClient.deleteClient).toHaveBeenCalledWith("c1");
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["clients"] });
  });

  it("editMutation rejects with 'Client data not loaded' when rawClient missing", async () => {
    const { Wrapper } = wrapper();
    const { result } = renderHook(
      () => useClientMutations("c1", undefined),
      { wrapper: Wrapper },
    );

    await expect(
      result.current.editMutation.mutateAsync({} as never),
    ).rejects.toThrow(/not loaded/);
  });
});
