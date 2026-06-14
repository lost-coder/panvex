import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { NodeSummaryCard } from "./NodeSummaryCard";

const base = {
  name: "edge-1",
  status: "error" as const,
  connections: 0,
  trafficBytes: 0,
  cpuPct: 9,
  memPct: 9,
  dcs: [],
};

describe("NodeSummaryCard status", () => {
  it("renders the NodeStateBadge pill + reason when state is provided", () => {
    render(<NodeSummaryCard {...base} state="down" reason="All upstreams down" />);
    expect(screen.getByText("DOWN")).toBeInTheDocument();
    expect(screen.getByText("All upstreams down")).toBeInTheDocument();
  });
  it("falls back to the beacon dot when no state is provided", () => {
    const { container } = render(<NodeSummaryCard {...base} />);
    expect(screen.queryByText("DOWN")).not.toBeInTheDocument();
    expect(container.querySelector("[class*='bg-status-error']")).toBeTruthy();
  });
});
