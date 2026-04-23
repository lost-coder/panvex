import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ClientFormSheet } from "./ClientFormSheet";
import type { ClientFormData } from "@/shared/api/types-pages/pages";

const emptyForm: ClientFormData = {
  name: "",
  userAdTag: "",
  expirationRfc3339: "",
  maxTcpConns: 0,
  maxUniqueIps: 0,
  dataQuotaBytes: 0,
  fleetGroupIds: [],
  agentIds: [],
};

const filledForm: ClientFormData = {
  name: "premium-users",
  userAdTag: "promo-channel",
  expirationRfc3339: "2026-12-31T00:00:00Z",
  maxTcpConns: 4,
  maxUniqueIps: 2,
  dataQuotaBytes: 1073741824,
  fleetGroupIds: [],
  agentIds: [],
};

describe("ClientFormSheet", () => {
  it("renders create mode title", () => {
    render(
      <ClientFormSheet
        mode="create"
        data={emptyForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByText("New Client")).toBeInTheDocument();
    expect(screen.getByText(/create client/i)).toBeInTheDocument();
  });

  it("renders edit mode title", () => {
    render(
      <ClientFormSheet
        mode="edit"
        data={filledForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByText("Edit Client")).toBeInTheDocument();
    expect(screen.getByText(/save changes/i)).toBeInTheDocument();
  });

  it("disables submit button when name is empty", () => {
    render(
      <ClientFormSheet
        mode="create"
        data={emptyForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    const submitBtn = screen.getByRole("button", { name: /create client/i });
    expect(submitBtn).toBeDisabled();
  });

  it("enables submit button when name is provided", () => {
    render(
      <ClientFormSheet
        mode="create"
        data={filledForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    const submitBtn = screen.getByRole("button", { name: /create client/i });
    expect(submitBtn).toBeEnabled();
  });

  it("calls onChange when client name changes", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ClientFormSheet
        mode="create"
        data={emptyForm}
        onChange={onChange}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    );

    const nameInput = screen.getByPlaceholderText("e.g. premium-users");
    await user.type(nameInput, "a");
    expect(onChange).toHaveBeenCalled();
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1]![0];
    expect(lastCall.name).toBe("a");
  });

  it("calls onCancel when cancel clicked", async () => {
    const user = userEvent.setup();
    const onCancel = vi.fn();
    render(
      <ClientFormSheet
        mode="create"
        data={emptyForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={onCancel}
      />,
    );

    await user.click(screen.getByRole("button", { name: /cancel/i }));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("calls onSubmit when form is submitted", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(
      <ClientFormSheet
        mode="create"
        data={filledForm}
        onChange={vi.fn()}
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: /create client/i }));
    expect(onSubmit).toHaveBeenCalledOnce();
  });

  it("shows loading state", () => {
    render(
      <ClientFormSheet
        mode="create"
        data={filledForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
        loading={true}
      />,
    );
    expect(screen.getByText("Saving...")).toBeInTheDocument();
  });

  it("emits TZ-safe expiration when a date is picked (P2-FE-04 / M-C9)", () => {
    // The prior impl called `new Date("YYYY-MM-DD").toISOString()`, which
    // treats the picked date as UTC midnight and shifts the calendar day
    // in any non-UTC timezone (e.g. in America/Los_Angeles 2024-03-15
    // would surface as 2024-03-14 after a round trip). The fix anchors
    // the picked day at noon UTC so the ISO string's date component
    // equals the picked day regardless of the runtime timezone.
    const onChange = vi.fn();
    render(
      <ClientFormSheet
        mode="create"
        data={{ ...emptyForm, name: "a" }}
        onChange={onChange}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    );

    const dateInput = document.querySelector('input[type="date"]') as HTMLInputElement;
    expect(dateInput).not.toBeNull();

    // fireEvent.change goes through React's synthetic event plumbing,
    // which is what the component's onChange handler reads from.
    fireEvent.change(dateInput, { target: { value: "2024-03-15" } });

    expect(onChange).toHaveBeenCalled();
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1]![0];
    expect(typeof lastCall.expirationRfc3339).toBe("string");
    // The ISO string's date portion must equal the picked day for every
    // reasonable timezone, not just UTC.
    expect(lastCall.expirationRfc3339.slice(0, 10)).toBe("2024-03-15");
    const parsed = new Date(lastCall.expirationRfc3339);
    expect(parsed.getUTCFullYear()).toBe(2024);
    expect(parsed.getUTCMonth()).toBe(2); // March (0-indexed)
    expect(parsed.getUTCDate()).toBe(15);
  });

  it("exposes aria-expanded + aria-controls on the Limits toggle (P2-FE-07 / M-F6)", async () => {
    const user = userEvent.setup();
    render(
      <ClientFormSheet
        mode="create"
        data={emptyForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    );

    const toggle = screen.getByRole("button", { name: /limits \(optional\)/i });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(toggle).toHaveAttribute("aria-controls", "client-form-limits-section");

    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");

    const region = document.getElementById("client-form-limits-section");
    expect(region).not.toBeNull();
  });

  it("shows error message", () => {
    render(
      <ClientFormSheet
        mode="create"
        data={filledForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
        error="Name already taken"
      />,
    );
    expect(screen.getByText("Name already taken")).toBeInTheDocument();
  });

  it("toggles a fleet group via the chip selector", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ClientFormSheet
        mode="create"
        data={{ ...emptyForm, name: "a" }}
        onChange={onChange}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
        fleetGroups={[{ id: "edge", label: "edge", agentCount: 3 }]}
      />,
    );

    const chip = screen.getByRole("button", { name: /edge/i });
    expect(chip).toHaveAttribute("aria-pressed", "false");
    await user.click(chip);

    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1]![0];
    expect(lastCall.fleetGroupIds).toEqual(["edge"]);
  });

  it("toggles an agent via the checkbox list", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ClientFormSheet
        mode="create"
        data={{ ...emptyForm, name: "a" }}
        onChange={onChange}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
        agents={[{ id: "agent-1", nodeName: "node-a", fleetGroupId: "edge" }]}
      />,
    );

    await user.click(screen.getByText("node-a"));

    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1]![0];
    expect(lastCall.agentIds).toEqual(["agent-1"]);
  });

  it("disables submit when selectors are shown but no target is picked", () => {
    render(
      <ClientFormSheet
        mode="create"
        data={filledForm}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
        fleetGroups={[{ id: "edge", label: "edge", agentCount: 2 }]}
      />,
    );
    const submit = screen.getByRole("button", { name: /create client/i });
    expect(submit).toBeDisabled();
  });

  it("enables submit when at least one deployment target is picked", () => {
    render(
      <ClientFormSheet
        mode="create"
        data={{ ...filledForm, fleetGroupIds: ["edge"] }}
        onChange={vi.fn()}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
        fleetGroups={[{ id: "edge", label: "edge", agentCount: 2 }]}
      />,
    );
    const submit = screen.getByRole("button", { name: /create client/i });
    expect(submit).toBeEnabled();
  });
});
