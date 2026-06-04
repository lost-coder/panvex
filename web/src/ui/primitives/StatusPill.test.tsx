import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusPill } from "./StatusPill";

describe("StatusPill", () => {
  it("renders the label and glyph", () => {
    render(<StatusPill tone="error" glyph="⛔" label="DOWN" />);
    expect(screen.getByText("DOWN")).toBeInTheDocument();
    expect(screen.getByText("⛔")).toBeInTheDocument();
  });
  it("applies the error tone classes", () => {
    render(<StatusPill tone="error" label="DOWN" />);
    const pill = screen.getByText("DOWN").closest("span");
    expect(pill?.className).toContain("bg-status-error");
  });
  it("hides the glyph from assistive tech (decorative)", () => {
    render(<StatusPill tone="warn" glyph="▲" label="DEGRADED" />);
    expect(screen.getByText("▲")).toHaveAttribute("aria-hidden", "true");
  });
});
