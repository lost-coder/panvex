import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ToastProvider, useToast } from "./ToastProvider";

// Helper component that exposes the toast API as buttons we can click
// from the test. Writing a harness like this is simpler than spinning
// up hook-testing utilities since ToastProvider is pure React state.
function Harness() {
  const toast = useToast();
  return (
    <div>
      <button onClick={() => toast.success("saved")}>push success</button>
      <button onClick={() => toast.error("boom")}>push error</button>
      <button onClick={() => toast.info("heads up")}>push info</button>
      <button
        onClick={() => toast.success("short", { duration: 50 })}
      >
        push short
      </button>
    </div>
  );
}

describe("ToastProvider", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("useToast throws when called outside provider", () => {
    // Suppress React's error boundary log spam — this failure is the
    // assertion's target, not a bug in the test harness.
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => render(<Harness />)).toThrow(/useToast\(\) called outside/);
    spy.mockRestore();
  });

  it("renders success/error/info toasts with correct roles", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <ToastProvider>
        <Harness />
      </ToastProvider>,
    );

    await user.click(screen.getByText("push success"));
    await user.click(screen.getByText("push error"));
    await user.click(screen.getByText("push info"));

    // Success + info share role=status; error uses role=alert.
    expect(screen.getByText("saved")).toBeInTheDocument();
    expect(screen.getByText("boom")).toBeInTheDocument();
    expect(screen.getByText("heads up")).toBeInTheDocument();

    const error = screen.getByText("boom").closest("[role]");
    expect(error?.getAttribute("role")).toBe("alert");
    const success = screen.getByText("saved").closest("[role]");
    expect(success?.getAttribute("role")).toBe("status");
  });

  it("dismiss button (×) removes the toast immediately", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <ToastProvider>
        <Harness />
      </ToastProvider>,
    );

    await user.click(screen.getByText("push info"));
    expect(screen.getByText("heads up")).toBeInTheDocument();

    const closeButtons = screen.getAllByLabelText("Закрыть уведомление");
    expect(closeButtons.length).toBeGreaterThan(0);
    await user.click(closeButtons[0]!);

    await waitFor(() => {
      expect(screen.queryByText("heads up")).not.toBeInTheDocument();
    });
  });

  it("Escape key dismisses the most recent toast", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <ToastProvider>
        <Harness />
      </ToastProvider>,
    );

    await user.click(screen.getByText("push success"));
    await user.click(screen.getByText("push info"));

    expect(screen.getByText("saved")).toBeInTheDocument();
    expect(screen.getByText("heads up")).toBeInTheDocument();

    // Escape pops the last (most recent) toast only.
    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(screen.queryByText("heads up")).not.toBeInTheDocument();
    });
    expect(screen.getByText("saved")).toBeInTheDocument();
  });

  it("auto-dismisses after the configured duration", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <ToastProvider>
        <Harness />
      </ToastProvider>,
    );

    await user.click(screen.getByText("push short"));
    expect(screen.getByText("short")).toBeInTheDocument();

    // Short duration = 50ms; close fires at duration+200.
    await act(async () => {
      vi.advanceTimersByTime(500);
    });

    await waitFor(() => {
      expect(screen.queryByText("short")).not.toBeInTheDocument();
    });
  });
});
