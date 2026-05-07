import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { SchemaEntry, ValuesEntry } from "./types";
import { RegistryField } from "./RegistryField";

function makeSchema(overrides: Partial<SchemaEntry> = {}): SchemaEntry {
  return {
    name: "test.field",
    class: "operational",
    type: "string",
    desc: "A test field",
    ...overrides,
  };
}

function makeValues(overrides: Partial<ValuesEntry> = {}): ValuesEntry {
  return {
    value: "hello",
    source: "db",
    locked: false,
    ...overrides,
  };
}

describe("RegistryField", () => {
  // --- type rendering ---

  it("renders text input for type=string", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string" })}
        values={makeValues({ value: "hello" })}
        onChange={vi.fn()}
      />,
    );
    const input = screen.getByRole("textbox", { name: "test.field" });
    expect(input).toBeInTheDocument();
    expect(input).toHaveValue("hello");
  });

  it("renders number input for type=int", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "int" })}
        values={makeValues({ value: 42 })}
        onChange={vi.fn()}
      />,
    );
    const input = screen.getByRole("spinbutton", { name: "test.field" });
    expect(input).toBeInTheDocument();
    expect(input).toHaveValue(42);
  });

  it("renders url input for type=url", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "url" })}
        values={makeValues({ value: "http://example.com" })}
        onChange={vi.fn()}
      />,
    );
    // url inputs have role=textbox in jsdom
    const input = screen.getByRole("textbox", { name: "test.field" });
    expect(input).toBeInTheDocument();
    expect((input as HTMLInputElement).type).toBe("url");
  });

  it("renders text input with placeholder for type=duration", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "duration" })}
        values={makeValues({ value: "30s" })}
        onChange={vi.fn()}
      />,
    );
    const input = screen.getByRole("textbox", { name: "test.field" });
    expect(input).toBeInTheDocument();
    expect(input).toHaveAttribute("placeholder", "e.g. 30s, 5m, 1h");
  });

  it("renders text input with placeholder for type=hostport", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "hostport" })}
        values={makeValues({ value: "0.0.0.0:8080" })}
        onChange={vi.fn()}
      />,
    );
    const input = screen.getByRole("textbox", { name: "test.field" });
    expect(input).toHaveAttribute("placeholder", "e.g. 0.0.0.0:8080");
  });

  it("renders toggle switch for type=bool", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "bool" })}
        values={makeValues({ value: true })}
        onChange={vi.fn()}
      />,
    );
    const toggle = screen.getByRole("switch");
    expect(toggle).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-checked", "true");
  });

  it("renders select for type=enum", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "enum", values: ["a", "b", "c"] })}
        values={makeValues({ value: "b" })}
        onChange={vi.fn()}
      />,
    );
    const select = screen.getByRole("combobox");
    expect(select).toBeInTheDocument();
    expect((select as HTMLSelectElement).value).toBe("b");
  });

  it("renders note for type=json", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "json" })}
        values={makeValues({ value: "{}" })}
        onChange={vi.fn()}
      />,
    );
    expect(
      screen.getByText(/Edit via the dedicated section below/i),
    ).toBeInTheDocument();
  });

  // --- locked ---

  it("locked=true disables input and shows env source pill", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string" })}
        values={makeValues({
          value: "x",
          locked: true,
          source: "env",
          env_var: "PANVEX_X",
        })}
        onChange={vi.fn()}
      />,
    );
    const input = screen.getByRole("textbox", { name: "test.field" });
    expect(input).toBeDisabled();
    expect(screen.getByText("Set via PANVEX_X")).toBeInTheDocument();
  });

  it("locked=true with source=config_file shows config.toml pill", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string" })}
        values={makeValues({
          value: "x",
          locked: true,
          source: "config_file",
        })}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText("Set in config.toml")).toBeInTheDocument();
  });

  it("locked=true with source=default shows Default pill", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string" })}
        values={makeValues({
          value: "x",
          locked: true,
          source: "default",
        })}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText("Default")).toBeInTheDocument();
  });

  // --- pending restart ---

  it("shows pending badge when pendingValue differs from value", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string" })}
        values={makeValues({
          value: "old",
          pending_restart: true,
          pending_value: "new",
        })}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText("restart pending")).toBeInTheDocument();
  });

  it("does not show pending badge when pendingValue equals value", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string" })}
        values={makeValues({
          value: "same",
          pending_restart: true,
          pending_value: "same",
        })}
        onChange={vi.fn()}
      />,
    );
    expect(screen.queryByText("restart pending")).not.toBeInTheDocument();
  });

  // --- error ---

  it("error prop renders red helper text", () => {
    render(
      <RegistryField
        schema={makeSchema({ type: "string" })}
        values={makeValues()}
        onChange={vi.fn()}
        error="Value is required"
      />,
    );
    expect(screen.getByText("Value is required")).toBeInTheDocument();
  });

  // --- onChange fires ---

  it("calls onChange when text input changes", async () => {
    const onChange = vi.fn();
    render(
      <RegistryField
        schema={makeSchema({ type: "string", name: "my.field" })}
        values={makeValues({ value: "" })}
        onChange={onChange}
      />,
    );
    const user = userEvent.setup();
    const input = screen.getByRole("textbox", { name: "my.field" });
    await user.type(input, "x");
    expect(onChange).toHaveBeenCalledWith("my.field", "x");
  });
});
