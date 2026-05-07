import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { SchemaEntry, ValuesEntry } from "./types";
import { RegistrySection } from "./RegistrySection";
import type { RegistrySectionField } from "./RegistrySection";

function makeField(
  schemaOverrides: Partial<SchemaEntry> = {},
  valuesOverrides: Partial<ValuesEntry> = {},
): RegistrySectionField {
  return {
    schema: {
      name: "auth.timeout",
      class: "operational",
      type: "int",
      desc: "Timeout in seconds",
      ...schemaOverrides,
    },
    values: {
      value: 30,
      source: "db",
      locked: false,
      ...valuesOverrides,
    },
  };
}

describe("RegistrySection", () => {
  it("renders title from namespaceLabels for known namespace", () => {
    render(
      <RegistrySection
        namespace="auth"
        fields={[makeField()]}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("heading", { name: /Authentication/i })).toBeInTheDocument();
  });

  it("falls back to namespace name as title for unknown namespace", () => {
    render(
      <RegistrySection
        namespace="foobar"
        fields={[makeField({ name: "foobar.x" })]}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("heading", { name: /foobar/i })).toBeInTheDocument();
  });

  it("renders all non-json fields and skips json fields", () => {
    const fields = [
      makeField({ name: "auth.timeout", type: "int" }),
      makeField({ name: "auth.max_attempts", type: "int" }),
      makeField({ name: "auth.policy", type: "json" }),
    ];
    render(
      <RegistrySection namespace="auth" fields={fields} onChange={vi.fn()} />,
    );
    // The two int fields render spinbuttons (number inputs)
    const spinboxes = screen.getAllByRole("spinbutton");
    expect(spinboxes).toHaveLength(2);
    // The json field is filtered out entirely by RegistrySection
    expect(screen.queryByText(/Edit via the dedicated section below/i)).not.toBeInTheDocument();
  });

  it("skips json-typed fields from rendered inputs", () => {
    const fields = [
      makeField({ name: "auth.cfg", type: "json" }),
    ];
    render(
      <RegistrySection namespace="auth" fields={fields} onChange={vi.fn()} />,
    );
    expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
  });

  it("propagates onChange from a field", async () => {
    const onChange = vi.fn();
    render(
      <RegistrySection
        namespace="auth"
        fields={[makeField({ name: "auth.limit", type: "string" }, { value: "" })]}
        onChange={onChange}
      />,
    );
    const user = userEvent.setup();
    const input = screen.getByRole("textbox", { name: "auth.limit" });
    await user.type(input, "5");
    expect(onChange).toHaveBeenCalledWith("auth.limit", "5");
  });

  it("renders description from namespaceLabels", () => {
    render(
      <RegistrySection
        namespace="auth"
        fields={[makeField()]}
        onChange={vi.fn()}
      />,
    );
    expect(
      screen.getByText(/Password policy, session timeouts, TOTP windows/i),
    ).toBeInTheDocument();
  });
});
