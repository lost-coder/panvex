import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StateBadge } from "./StateBadge";

describe("StateBadge", () => {
  it("renders a quiet glyph chip (no label) for the ok tone", () => {
    render(<StateBadge tone="ok" glyph="✓" label="ACTIVE" />);
    expect(screen.queryByText("ACTIVE")).not.toBeInTheDocument();
    // ok tone renders a quiet aria-hidden chip with a lucide check icon
    // (replaced the unicode ✓ glyph, which rendered as a dark emoji on
    // some platforms), not a labelled pill.
    const chip = document.querySelector('span[aria-hidden="true"]');
    expect(chip).not.toBeNull();
    expect(chip?.querySelector("svg")).toBeInTheDocument();
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
