import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mock the router's useNavigate + the api client. We only care that the
// handler clears the cache + calls navigate + surfaces the toast; we
// don't want TanStack Router to actually mount a route tree here.
const navigateSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
}));

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api");
  return {
    ...actual,
    apiClient: {
      me: vi.fn().mockResolvedValue({
        id: "u1",
        username: "admin",
        role: "admin",
        totp_enabled: false,
      }),
    },
  };
});

import { AuthProvider, useAuth } from "./AuthProvider";
import { SESSION_EXPIRED_EVENT } from "@/lib/api";
import { ToastProvider } from "./ToastProvider";

function AuthSpy() {
  const auth = useAuth();
  return (
    <div>
      <span data-testid="authed">{auth.isAuthenticated ? "yes" : "no"}</span>
      <span data-testid="user">{auth.user?.username ?? "none"}</span>
    </div>
  );
}

function wrap(ui: React.ReactNode) {
  // Every test gets a fresh QueryClient so cache state doesn't bleed.
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return {
    qc,
    ui: (
      <ToastProvider>
        <QueryClientProvider client={qc}>
          <AuthProvider>{ui}</AuthProvider>
        </QueryClientProvider>
      </ToastProvider>
    ),
  };
}

describe("AuthProvider", () => {
  beforeEach(() => {
    navigateSpy.mockReset();
    // jsdom defaults pathname to '/' which is what we want for the
    // "not already on /login" branch.
    window.history.replaceState({}, "", "/");
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it("clears cache and navigates to /login on SESSION_EXPIRED_EVENT", async () => {
    const { qc, ui } = wrap(<AuthSpy />);

    // Seed an arbitrary cache entry so we can prove clear() ran.
    qc.setQueryData(["stale"], { touched: true });

    render(ui);

    await act(async () => {
      window.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT));
    });

    expect(qc.getQueryData(["stale"])) .toBeUndefined();
    expect(navigateSpy).toHaveBeenCalledWith({ to: "/login" });
  });

  it("does NOT navigate when already on /login", async () => {
    window.history.replaceState({}, "", "/login");
    const { ui } = wrap(<AuthSpy />);
    render(ui);

    await act(async () => {
      window.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT));
    });

    expect(navigateSpy).not.toHaveBeenCalled();
  });
});
