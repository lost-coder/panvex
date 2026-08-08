import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { ConfigTreeField } from "./ConfigTreeField";
import type { TreeField } from "./buildTree";

const base: TreeField = { path: "general.log_level", value: "normal", observed: "silent",
  drifted: false, locked: false, readonly: false, unknown: false, present: true,
  entry: { path: "general.log_level", section: "general",
    type: "select", applyMode: "hot", options: ["debug","verbose","normal","silent"],
    en: "Runtime logging verbosity", ru: "Уровень детализации логов" } };

describe("ConfigTreeField", () => {
  it("renders the raw key as label", () => {
    render(<ConfigTreeField field={base} onChange={() => {}} />);
    expect(screen.getByText("log_level")).toBeInTheDocument();
  });
  it("associates the label with the actual focusable control (a11y)", () => {
    render(<ConfigTreeField field={base} onChange={() => {}} />);
    // getByLabelText matches on the accessible name (label text content),
    // so "log_level" (the raw key) resolves the select even though the
    // "Live" badge and ℹ tooltip glyph are also inside the label.
    const control = screen.getByLabelText(/log_level/);
    expect(control.tagName).toBe("SELECT");
    expect(control.id).toBeTruthy();
    const label = document.querySelector(`label[for="${control.id}"]`);
    expect(label).not.toBeNull();
    expect(label).toHaveTextContent("log_level");
  });

  it("emits onChange for an editable field", () => {
    const onChange = vi.fn();
    render(<ConfigTreeField field={base} onChange={onChange} />);
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "debug" } });
    expect(onChange).toHaveBeenCalledWith("general.log_level", "debug");
  });
  it("does not emit for a locked field", () => {
    const onChange = vi.fn();
    render(<ConfigTreeField field={{ ...base, locked: true }} onChange={onChange} />);
    // control disabled; lock note shown
    expect(screen.getByText(/set by group|управляется группой/i)).toBeInTheDocument();
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "debug" } });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("shows a process-owned note instead of unknown for process-owned paths", () => {
    render(
      <ConfigTreeField
        field={{
          path: "general.data_path",
          value: "/var/lib/telemt",
          observed: "/var/lib/telemt",
          drifted: false,
          locked: false,
          readonly: true,
          unknown: true,
          present: true,
          entry: null,
        }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText(/process start|запуске процесса/i)).toBeInTheDocument();
    expect(screen.queryByText(/unknown parameter|неизвестный параметр/i)).not.toBeInTheDocument();
  });

  it("shows the unknown-parameter note for a genuinely unknown, non-process-owned path", () => {
    render(
      <ConfigTreeField
        field={{
          path: "general.mystery_flag",
          value: 1,
          observed: 1,
          drifted: false,
          locked: false,
          readonly: true,
          unknown: true,
          present: true,
          entry: null,
        }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText(/unknown parameter|неизвестный параметр/i)).toBeInTheDocument();
  });

  // P4-T4: per-field drift-resolution actions.
  const drifted: TreeField = { ...base, drifted: true, value: "normal", observed: "silent" };

  it("does not render drift-action buttons when no handlers are given, even when drifted", () => {
    render(<ConfigTreeField field={drifted} onChange={() => {}} />);
    expect(screen.queryByRole("button", { name: /accept node/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /revert to panel/i })).not.toBeInTheDocument();
  });

  it("renders and wires the accept-node and revert-to-panel actions on a drifted field", () => {
    const onAcceptNode = vi.fn();
    const onRevertPanel = vi.fn();
    render(
      <ConfigTreeField
        field={drifted}
        onChange={() => {}}
        onAcceptNode={onAcceptNode}
        onRevertPanel={onRevertPanel}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Accept node value" }));
    expect(onAcceptNode).toHaveBeenCalledWith("general.log_level");
    fireEvent.click(screen.getByRole("button", { name: "Revert to panel value" }));
    expect(onRevertPanel).toHaveBeenCalledWith("general.log_level");
  });

  it("resolves the {{name}} placeholder in the locked-by-group note from groupName, falling back when omitted", () => {
    const { rerender } = render(
      <ConfigTreeField field={{ ...base, locked: true }} onChange={() => {}} groupName="edge-fleet" />,
    );
    expect(screen.getByText(/set by group edge-fleet/i)).toBeInTheDocument();

    rerender(<ConfigTreeField field={{ ...base, locked: true }} onChange={() => {}} />);
    expect(screen.queryByText(/\{\{name\}\}/)).not.toBeInTheDocument();
    expect(screen.getByText(/set by group/i)).toBeInTheDocument();
  });

  it("незаданное строковое поле показывает дефолт плейсхолдером и подпись «не задано»", () => {
    render(
      <ConfigTreeField
        field={{
          entry: {
            path: "general.proxy_config_v4_url",
            section: "general",
            type: "string",
            applyMode: "reload",
            default: "https://core.telegram.org/getProxyConfig",
            en: "", ru: "",
          },
          path: "general.proxy_config_v4_url",
          value: undefined,
          observed: undefined,
          drifted: false,
          locked: false,
          readonly: false,
          unknown: false,
          present: false,
        }}
        onChange={() => {}}
      />,
    );

    const input = screen.getByRole("textbox");
    expect(input).toHaveValue("");
    expect(input).toHaveAttribute("placeholder", "https://core.telegram.org/getProxyConfig");
    expect(screen.getByText(/не задано|Not set/i)).toBeInTheDocument();
  });

  it("заданное поле не показывает подпись «не задано»", () => {
    render(
      <ConfigTreeField
        field={{
          entry: {
            path: "general.log_level", section: "general", type: "string",
            applyMode: "hot", en: "", ru: "",
          },
          path: "general.log_level",
          value: "silent",
          observed: "silent",
          drifted: false, locked: false, readonly: false, unknown: false,
          present: true,
        }}
        onChange={() => {}}
      />,
    );
    expect(screen.queryByText(/не задано|Not set/i)).not.toBeInTheDocument();
  });
});
