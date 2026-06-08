import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StateBadge } from "./StateBadge";

describe("StateBadge", () => {
  it("renders a quiet glyph chip (no label) for the ok tone", () => {
    render(<StateBadge tone="ok" glyph="✓" label="ACTIVE" />);
    expect(screen.queryByText("ACTIVE")).not.toBeInTheDocument();
    expect(screen.getByText("✓")).toHaveAttribute("aria-hidden", "true");
  });
  it("renders a loud pill with the label for a non-ok tone", () => {
    render(<StateBadge tone="error" glyph="⛔" label="EXPIRED" />);
    const pill = screen.getByText("EXPIRED").closest("span");
    expect(pill?.className).toContain("bg-status-error");
  });
  it("renders a neutral pill for the neutral tone", () => {
    render(<StateBadge tone="neutral" glyph="●" label="DISABLED" />);
    expect(screen.getByText("DISABLED")).toBeInTheDocument();
  });
});
