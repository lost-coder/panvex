import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { ConfigTreeField } from "./ConfigTreeField";
import type { TreeField } from "./buildTree";

const base: TreeField = { path: "general.log_level", value: "normal", observed: "silent",
  drifted: false, locked: false, readonly: false, unknown: false,
  entry: { path: "general.log_level", section: "general", key: "log_level",
    type: "select", applyMode: "hot", options: ["debug","verbose","normal","silent"],
    en: "Runtime logging verbosity", ru: "Уровень детализации логов" } };

describe("ConfigTreeField", () => {
  it("renders the raw key as label", () => {
    render(<ConfigTreeField field={base} onChange={() => {}} />);
    expect(screen.getByText("log_level")).toBeInTheDocument();
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
          entry: null,
        }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText(/unknown parameter|неизвестный параметр/i)).toBeInTheDocument();
  });
});
