import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { SchemaEntry, ValuesEntry } from "./types";
import { RegistryField } from "./RegistryField";

function makeSchema(overrides: Partial<SchemaEntry> = {}): SchemaEntry {
  return { name: "test.field", class: "operational", type: "string", desc: "A test field", ...overrides };
}
function makeValues(overrides: Partial<ValuesEntry> = {}): ValuesEntry {
  return { value: "hello", source: "db", locked: false, ...overrides };
}

describe("RegistryField", () => {
  // --- type rendering (unchanged behaviour) ---

  it("renders text input for type=string", () => {
    render(<RegistryField schema={makeSchema({ type: "string" })} values={makeValues({ value: "hello" })} onChange={vi.fn()} />);
    const input = screen.getByRole("textbox", { name: "test.field" });
    expect(input).toBeInTheDocument();
    expect(input).toHaveValue("hello");
  });

  it("renders number input for type=int", () => {
    render(<RegistryField schema={makeSchema({ type: "int" })} values={makeValues({ value: 42 })} onChange={vi.fn()} />);
    expect(screen.getByRole("spinbutton", { name: "test.field" })).toHaveValue(42);
  });

  it("renders text input with placeholder for type=duration", () => {
    render(<RegistryField schema={makeSchema({ type: "duration" })} values={makeValues({ value: "30s" })} onChange={vi.fn()} />);
    expect(screen.getByRole("textbox", { name: "test.field" })).toHaveAttribute("placeholder", "e.g. 30s, 5m, 1h");
  });

  it("renders toggle switch for type=bool", () => {
    render(<RegistryField schema={makeSchema({ type: "bool" })} values={makeValues({ value: true })} onChange={vi.fn()} />);
    expect(screen.getByRole("switch")).toHaveAttribute("aria-checked", "true");
  });

  it("renders select for type=enum", () => {
    render(<RegistryField schema={makeSchema({ type: "enum", values: ["a", "b", "c"] })} values={makeValues({ value: "b" })} onChange={vi.fn()} />);
    expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("b");
  });

  it("renders note for type=json", () => {
    render(<RegistryField schema={makeSchema({ type: "json" })} values={makeValues({ value: "{}" })} onChange={vi.fn()} />);
    expect(screen.getByText(/Edit via the dedicated section below/i)).toBeInTheDocument();
  });

  // --- indicator: none for live/editable ---

  it("renders no indicator icon for a live field", () => {
    render(<RegistryField schema={makeSchema({ apply: "live" })} values={makeValues({ apply: "live" })} onChange={vi.fn()} />);
    expect(screen.queryByLabelText("Read-only")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Restart-related")).not.toBeInTheDocument();
  });

  // --- indicator: env-override (amber lock, disabled) ---

  it("env-override disables input and shows a read-only icon", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string" })}
        values={makeValues({ overridden_by_env: true, locked: true, source: "env", env_var: "PANVEX_HTTP_ADDR" })}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox", { name: "test.field" })).toBeDisabled();
    expect(screen.getByLabelText("Read-only")).toBeInTheDocument();
  });

  // --- indicator: config-managed (grey lock, disabled) ---

  it("config-managed locked field disables input and shows a read-only icon", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string", class: "bootstrap", apply: "config" })}
        values={makeValues({ apply: "config", locked: true })}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox", { name: "test.field" })).toBeDisabled();
    expect(screen.getByLabelText("Read-only")).toBeInTheDocument();
  });

  // --- indicator: needs restart (amber restart icon, editable) ---

  it("needs-restart field shows a restart-related icon and stays editable", () => {
    render(<RegistryField schema={makeSchema({ apply: "restart" })} values={makeValues({ apply: "restart" })} onChange={vi.fn()} />);
    expect(screen.getByLabelText("Restart-related")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "test.field" })).not.toBeDisabled();
  });

  // --- indicator: pending restart (spinning) ---

  it("pending restart shows the restart icon with the spin class", () => {
    const { container } = render(
      <RegistryField schema={makeSchema({ apply: "restart" })} values={makeValues({ apply: "restart", value: "old", pending_restart: true, pending_value: "new" })} onChange={vi.fn()} />,
    );
    expect(screen.getByLabelText("Restart-related")).toBeInTheDocument();
    expect(container.querySelector(".animate-spin")).not.toBeNull();
  });

  it("does not spin when pending_value equals value", () => {
    const { container } = render(
      <RegistryField schema={makeSchema({ apply: "restart" })} values={makeValues({ apply: "restart", value: "same", pending_restart: true, pending_value: "same" })} onChange={vi.fn()} />,
    );
    expect(container.querySelector(".animate-spin")).toBeNull();
  });

  // --- hideIndicators ---

  it("hideIndicators suppresses bar and icon but keeps input disabled when locked", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string", class: "bootstrap" })}
        values={makeValues({ locked: true, source: "config_file" })}
        onChange={vi.fn()}
        hideIndicators
      />,
    );
    expect(screen.queryByLabelText("Read-only")).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "test.field" })).toBeDisabled();
  });

  // --- error ---

  it("error prop renders red helper text", () => {
    render(<RegistryField schema={makeSchema({ type: "string" })} values={makeValues()} onChange={vi.fn()} error="Value is required" />);
    expect(screen.getByText("Value is required")).toBeInTheDocument();
  });

  // --- onChange ---

  it("calls onChange when text input changes", async () => {
    const onChange = vi.fn();
    render(<RegistryField schema={makeSchema({ type: "string", name: "my.field" })} values={makeValues({ value: "" })} onChange={onChange} />);
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "my.field" }), "x");
    expect(onChange).toHaveBeenCalledWith("my.field", "x");
  });
});
