import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

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

  it("kicks off immediately for hot-only changes (no dialog)", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton changedPaths={["general.log_level"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    expect(onApply).toHaveBeenCalledTimes(1);
    // The button is kickoff-only now — outcome surfacing lives in the caller,
    // so it must not toast success/error itself.
    expect(toastApi.success).not.toHaveBeenCalled();
    expect(toastApi.error).not.toHaveBeenCalled();
  });

  it("opens the restart-warning dialog for restart changes and applies on confirm", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton changedPaths={["censorship.tls_domain"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    // Warning dialog is shown; onApply not yet called.
    expect(screen.getByText("Restart required")).toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();

    // Confirm -> apply fires.
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(onApply).toHaveBeenCalledTimes(1));
  });

  it("does not apply when the restart dialog is cancelled", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton changedPaths={["censorship.tls_domain"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onApply).not.toHaveBeenCalled();
  });

  it("disables the button while the kickoff request is in flight", async () => {
    // A never-resolving kickoff keeps the in-flight state latched so the
    // button stays disabled.
    let release!: () => void;
    const onApply = vi.fn(
      () => new Promise<void>((resolve) => (release = resolve)),
    );
    render(
      <ApplyConfigButton changedPaths={["general.log_level"]} onApply={onApply} />,
    );
    const button = screen.getByRole("button", { name: "Apply to node" });
    await userEvent.click(button);
    await waitFor(() => expect(button).toBeDisabled());
    // Settling the kickoff re-enables the button.
    release();
    await waitFor(() => expect(button).not.toBeDisabled());
  });
});
