import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { TransportBadge } from "./TransportBadge";

describe("TransportBadge", () => {
  it("renders 'ME H/T' for me mode", () => {
    render(<TransportBadge mode="me" healthy={4} total={4} severity="ok" />);
    expect(screen.getByText(/ME 4\/4/i)).toBeInTheDocument();
  });
  it("renders 'Direct H/T' for direct mode", () => {
    render(<TransportBadge mode="direct" healthy={3} total={3} severity="ok" />);
    expect(screen.getByText(/Direct 3\/3/i)).toBeInTheDocument();
  });
  it("renders 'Fallback H/T' for fallback mode", () => {
    render(<TransportBadge mode="fallback" healthy={3} total={3} severity="warn" />);
    expect(screen.getByText(/Fallback 3\/3/i)).toBeInTheDocument();
  });
  it("renders 'ME down' for me_down mode (no totals)", () => {
    render(<TransportBadge mode="me_down" healthy={0} total={0} severity="critical" />);
    expect(screen.getByText(/ME down/i)).toBeInTheDocument();
  });
  it("applies the severity tone class", () => {
    const { container } = render(
      <TransportBadge mode="direct" healthy={0} total={3} severity="critical" />
    );
    expect(container.firstChild).toHaveClass(/critical|error|red/i);
  });
});
