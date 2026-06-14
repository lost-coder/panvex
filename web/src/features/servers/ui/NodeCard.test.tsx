import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { NodeCard } from "./NodeCard";

const base = {
  name: "edge-1",
  status: "error" as const,
  mode: "me" as const,
  healthyUpstreams: 0,
  totalUpstreams: 1,
  severity: "bad" as const,
  cpu: 9,
  mem: 9,
  clients: 0,
  region: "Global",
};

describe("NodeCard status", () => {
  it("renders a pill + reason when state is provided", () => {
    render(<NodeCard {...base} state="down" reason="All upstreams down" />);
    expect(screen.getByText("DOWN")).toBeInTheDocument();
    expect(screen.getByText("All upstreams down")).toBeInTheDocument();
  });
  it("falls back to the status dot when no state is provided", () => {
    const { container } = render(<NodeCard {...base} />);
    expect(screen.queryByText("DOWN")).not.toBeInTheDocument();
    expect(container.querySelector("[class*='bg-status-error']")).toBeTruthy();
  });
});
