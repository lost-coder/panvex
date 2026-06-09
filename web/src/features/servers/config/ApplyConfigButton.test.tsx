import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ApplyResult } from "@/shared/api/schemas/config";

// Mock the global toast channel the same way the settings/clients tests do.
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

import { ApplyConfigButton } from "./ApplyConfigButton";

function okResult(applied = 1): ApplyResult {
  return { applied, failed: "", error: "" };
}

describe("ApplyConfigButton", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // jsdom doesn't implement <dialog>.showModal/close; stub them so the
    // ConfirmDialog effect doesn't throw when toggling `open`.
    HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
      this.open = true;
    });
    HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
      this.open = false;
    });
  });

  it("applies immediately for hot-only changes (no dialog) and toasts success", async () => {
    const onApply = vi.fn().mockResolvedValue(okResult(2));
    render(
      <ApplyConfigButton changedPaths={["general.log_level"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    expect(onApply).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(toastApi.success).toHaveBeenCalledTimes(1));
    expect(toastApi.success).toHaveBeenCalledWith("Applied to 2 node(s)");
  });

  it("opens the restart-warning dialog for restart changes and applies on confirm", async () => {
    const onApply = vi.fn().mockResolvedValue(okResult(1));
    render(
      <ApplyConfigButton changedPaths={["censorship.tls_domain"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    // Warning dialog is shown; onApply not yet called.
    expect(screen.getByText("Restart required")).toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();

    // Confirm -> apply fires.
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(onApply).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(toastApi.success).toHaveBeenCalledTimes(1));
  });

  it("does not apply when the restart dialog is cancelled", async () => {
    const onApply = vi.fn().mockResolvedValue(okResult(1));
    render(
      <ApplyConfigButton changedPaths={["censorship.tls_domain"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onApply).not.toHaveBeenCalled();
  });

  it("toasts an error when the result reports a failure", async () => {
    const onApply = vi
      .fn()
      .mockResolvedValue({ applied: 0, failed: "node-1", error: "boom" } satisfies ApplyResult);
    render(
      <ApplyConfigButton changedPaths={["general.log_level"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    await waitFor(() => expect(toastApi.error).toHaveBeenCalledTimes(1));
    expect(toastApi.error).toHaveBeenCalledWith("Failed on node-1: boom");
    expect(toastApi.success).not.toHaveBeenCalled();
  });

  it("shows the error toast (not success) on the partial-failure path", async () => {
    // applied:0 with a non-empty failed/error must surface as an error
    // toast, never the success toast.
    const onApply = vi
      .fn()
      .mockResolvedValue({ applied: 0, failed: "agent-x", error: "boom" } satisfies ApplyResult);
    render(
      <ApplyConfigButton changedPaths={["general.log_level"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    await waitFor(() => expect(toastApi.error).toHaveBeenCalledTimes(1));
    expect(toastApi.error).toHaveBeenCalledWith("Failed on agent-x: boom");
    expect(toastApi.success).not.toHaveBeenCalled();
  });
});
