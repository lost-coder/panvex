import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { UpstreamHealthCard } from "./UpstreamHealthCard";

describe("UpstreamHealthCard", () => {
  it("renders H/T headline", () => {
    render(
      <UpstreamHealthCard
        healthy={3}
        total={3}
        failRatePct5m={0}
        failRateKnown={true}
        currentDirectConnections={5}
      />,
    );
    expect(screen.getByText("3/3")).toBeInTheDocument();
  });
  it("renders 'unknown' for fail rate when known=false", () => {
    render(
      <UpstreamHealthCard
        healthy={3}
        total={3}
        failRatePct5m={0}
        failRateKnown={false}
        currentDirectConnections={0}
      />,
    );
    expect(screen.getByText(/unknown/i)).toBeInTheDocument();
  });
});
