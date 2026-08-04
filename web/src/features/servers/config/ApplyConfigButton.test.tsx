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
    // Hot-only changes apply immediately with an empty policy — the panel
    // defaults it server-side — and no confirm dialog.
    expect(onApply).toHaveBeenCalledWith({});
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    // The button is kickoff-only now — outcome surfacing lives in the caller,
    // so it must not toast success/error itself.
    expect(toastApi.success).not.toHaveBeenCalled();
    expect(toastApi.error).not.toHaveBeenCalled();
  });

  it("offers a session-policy choice for reload changes, defaulting to drain, and applies on confirm", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton changedPaths={["censorship.tls_domain"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    // The dialog no longer talks about a restart, and it offers the
    // drain/instant choice with the default set to drain.
    expect(screen.queryByText("Restart required")).not.toBeInTheDocument();
    expect(screen.queryByText(/restart/i)).not.toBeInTheDocument();
    const drainRadio = screen.getByRole("radio", { name: /drain/i });
    const instantRadio = screen.getByRole("radio", { name: /instant/i });
    expect(drainRadio).toBeChecked();
    expect(instantRadio).not.toBeChecked();
    expect(onApply).not.toHaveBeenCalled();

    // Confirm -> apply fires with the default drain policy.
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        reload_mode: "drain",
        reload_timeout_secs: 30,
      }),
    );
  });

  it("applies with instant policy when the operator picks instant", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton changedPaths={["censorship.tls_domain"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    await userEvent.click(screen.getByRole("radio", { name: /instant/i }));
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({ reload_mode: "instant" }),
    );
  });

  it("does not apply when the reload dialog is cancelled", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton changedPaths={["censorship.tls_domain"]} onApply={onApply} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onApply).not.toHaveBeenCalled();
  });

  it("warns about drifted fields before applying and can be cancelled", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton
        changedPaths={["general.log_level"]}
        driftedFields={["censorship.tls_domain"]}
        onApply={onApply}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /apply/i }));
    expect(screen.getByText(/censorship\.tls_domain/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /cancel/i }));
    expect(onApply).not.toHaveBeenCalled();
  });

  it("applies when the drift warning is confirmed", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton
        changedPaths={["general.log_level"]}
        driftedFields={["censorship.tls_domain"]}
        onApply={onApply}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /apply/i }));
    expect(screen.getByText(/censorship\.tls_domain/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(onApply).toHaveBeenCalledTimes(1));
  });

  it("does not warn when there is no drift", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton
        changedPaths={["general.log_level"]}
        driftedFields={[]}
        onApply={onApply}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /apply/i }));
    expect(onApply).toHaveBeenCalledTimes(1);
  });

  it("shows a single combined dialog with both drift warning and the session-policy choice", async () => {
    const onApply = vi.fn().mockResolvedValue(undefined);
    render(
      <ApplyConfigButton
        changedPaths={["censorship.tls_domain"]}
        driftedFields={["censorship.tls_domain"]}
        onApply={onApply}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply to node" }));
    // Only one dialog, carrying both the drift message and the drain/instant
    // choice — two stacked modals is exactly what the drift warning avoids.
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
    expect(screen.getByText(/censorship\.tls_domain/)).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /drain/i })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /instant/i })).toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(onApply).toHaveBeenCalledWith({
        reload_mode: "drain",
        reload_timeout_secs: 30,
      }),
    );
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
