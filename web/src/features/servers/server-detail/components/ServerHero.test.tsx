import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { mockDirectServer } from "@/test/fixtures";

import { ServerHero } from "./ServerHero";

// R-4 (R10b Task 4): the hero surfaces a warning badge when the live
// connection direction disagrees with the persisted transport mode.

function renderHero(transportDrift: boolean) {
  render(
    <ServerHero
      server={mockDirectServer({ transportDrift })}
      pulseWord="steady"
      relativeTime={null}
      relativeTimeStale={false}
    />,
  );
}

describe("ServerHero transport drift badge", () => {
  it("renders the drift badge when transportDrift is true", () => {
    renderHero(true);
    expect(screen.getByText(/Transport out of sync/i)).toBeInTheDocument();
  });

  it("omits the drift badge when transportDrift is false", () => {
    renderHero(false);
    expect(screen.queryByText(/Transport out of sync/i)).not.toBeInTheDocument();
  });
});
