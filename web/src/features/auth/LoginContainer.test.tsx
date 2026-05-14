import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api/api";
import { LoginContainer } from "./LoginContainer";

const navigateSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useRouter: () => ({ navigate: navigateSpy }),
}));

const loginMock = vi.fn();
vi.mock("@/shared/api/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/shared/api/api")>();
  return {
    ...actual,
    apiClient: {
      ...actual.apiClient,
      login: (...args: unknown[]) => loginMock(...args),
    },
  };
});

// Minimal LoginPage mock: exposes props so the test can drive submits
// without needing the UI-kit shell.
vi.mock("@/features/auth/LoginPage", () => ({
  LoginPage: (props: {
    onCredentials: (u: string, p: string) => void;
    error?: string;
  }) => (
    <div>
      <button
        data-testid="submit"
        onClick={() => props.onCredentials("admin", "hunter2")}
      >
        {"submit"}
      </button>
      <span data-testid="error">{props.error ?? ""}</span>
    </div>
  ),
}));

beforeEach(() => {
  navigateSpy.mockReset();
  loginMock.mockReset();
});

describe("LoginContainer — transient login failures (2.1)", () => {
  it("shows retry-friendly copy on audit_persist_unavailable", async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError("audit log unavailable, please retry", "audit_persist_unavailable"),
    );
    render(<LoginContainer />);
    fireEvent.click(screen.getByTestId("submit"));

    await screen.findByText(/temporarily unavailable/i);
    expect(navigateSpy).not.toHaveBeenCalled();
  });

  it("shows retry-friendly copy on session_store_unavailable", async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError("session store unavailable", "session_store_unavailable"),
    );
    render(<LoginContainer />);
    fireEvent.click(screen.getByTestId("submit"));

    await screen.findByText(/temporarily unavailable/i);
  });

  it("passes through the raw server message for plain login failures", async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError("invalid credentials", "invalid_credentials"),
    );
    render(<LoginContainer />);
    fireEvent.click(screen.getByTestId("submit"));

    await screen.findByText(/invalid credentials/);
  });
});
