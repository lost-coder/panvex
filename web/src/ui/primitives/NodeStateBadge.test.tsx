import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { NodeStateBadge } from "./NodeStateBadge";

describe("NodeStateBadge", () => {
  it("renders a quiet ok glyph chip (no label word) for ok", () => {
    render(<NodeStateBadge state="ok" label="OK" />);
    expect(screen.queryByText("OK")).not.toBeInTheDocument();
    // ok tone renders a quiet aria-hidden chip with a lucide check icon
    // (replaced the unicode ✓ glyph), not a labelled pill.
    const chip = document.querySelector('span[aria-hidden="true"]');
    expect(chip).not.toBeNull();
    expect(chip?.querySelector("svg")).toBeInTheDocument();
  });
  it("renders a loud pill with the label for a problem state", () => {
    render(<NodeStateBadge state="down" label="DOWN" />);
    const pill = screen.getByText("DOWN").closest("span");
    expect(pill?.className).toContain("bg-status-error");
  });
});
