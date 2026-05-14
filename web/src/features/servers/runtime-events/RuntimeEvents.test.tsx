import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { RuntimeEvents } from "./RuntimeEvents";

vi.mock("./useAgentRuntimeEvents", () => ({
  useAgentRuntimeEvents: () => ({
    events: [
      { ts: "2026-05-14T13:00:02Z", level: "error" as const, message: "error-msg", fields: { agent_id: "x" } },
      { ts: "2026-05-14T13:00:01Z", level: "warn" as const, message: "warn-msg" },
      { ts: "2026-05-14T13:00:00Z", level: "info" as const, message: "info-msg" },
    ],
    isLoading: false,
    isLive: false,
  }),
}));

function wrap(node: React.ReactNode) {
  const qc = new QueryClient();
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

function openFold() {
  // RuntimeEvents lives inside a collapsed-by-default <Fold>; expand it
  // so the filter buttons and event list become reachable.
  fireEvent.click(screen.getByRole("button", { name: /recent events/i }));
}

describe("RuntimeEvents", () => {
  it("renders all events newest-first", () => {
    render(wrap(<RuntimeEvents agentId="agent-1" />));
    openFold();
    expect(screen.getByText("error-msg")).toBeInTheDocument();
    expect(screen.getByText("warn-msg")).toBeInTheDocument();
    expect(screen.getByText("info-msg")).toBeInTheDocument();
  });

  it("level toggle hides excluded levels", () => {
    render(wrap(<RuntimeEvents agentId="agent-1" />));
    openFold();
    // Click the Info toggle to turn it off
    fireEvent.click(screen.getByRole("button", { name: /info/i }));
    expect(screen.queryByText("info-msg")).not.toBeInTheDocument();
    expect(screen.getByText("warn-msg")).toBeInTheDocument();
  });
});
