import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SettingsLegend } from "./SettingsLegend";

describe("SettingsLegend", () => {
  it("renders the four indicator legend entries", () => {
    render(<SettingsLegend />);
    expect(screen.getByText("set in config.toml (read-only)")).toBeInTheDocument();
    expect(screen.getByText("overridden by env")).toBeInTheDocument();
    expect(screen.getByText("needs restart")).toBeInTheDocument();
    expect(screen.getByText("restart pending")).toBeInTheDocument();
  });
});
