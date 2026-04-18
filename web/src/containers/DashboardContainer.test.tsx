import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const navigateSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
}));

vi.mock("@lost-coder/panvex-ui", () => ({
  Spinner: () => <div data-testid="spinner" />,
}));

vi.mock("@lost-coder/panvex-ui/pages", () => ({
  DashboardPage: (props: {
    overview: {
      attentionNodes: { id: string; updateAvailable?: boolean }[];
      healthyNodes: { id: string; updateAvailable?: boolean }[];
    };
    pendingDiscoveredCount: number;
  }) => (
    <div>
      <span data-testid="attention">{props.overview.attentionNodes.length}</span>
      <span data-testid="healthy">{props.overview.healthyNodes.length}</span>
      <span data-testid="pending">{props.pendingDiscoveredCount}</span>
      <span data-testid="has-update">
        {
          [...props.overview.attentionNodes, ...props.overview.healthyNodes]
            .some((n) => n.updateAvailable) ? "yes" : "no"
        }
      </span>
    </div>
  ),
}));

const useDashboardDataMock = vi.fn();
vi.mock("@/hooks/useDashboardData", () => ({
  useDashboardData: () => useDashboardDataMock(),
}));

const useDiscoveredClientsMock = vi.fn();
vi.mock("@/hooks/useDiscoveredClients", () => ({
  useDiscoveredClients: () => useDiscoveredClientsMock(),
}));

vi.mock("@/hooks/useClientCreate", () => ({
  useClientCreate: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
    error: null,
  }),
}));

const useUpdatesMock = vi.fn();
vi.mock("@/hooks/useUpdates", () => ({
  useUpdates: () => useUpdatesMock(),
}));

import { DashboardContainer } from "./DashboardContainer";

describe("DashboardContainer", () => {
  beforeEach(() => {
    navigateSpy.mockReset();
  });

  it("renders spinner while loading", () => {
    useDashboardDataMock.mockReturnValue({
      overview: null,
      timeline: null,
      agentVersions: {},
      isLoading: true,
    });
    useDiscoveredClientsMock.mockReturnValue({ discoveredClients: [] });
    useUpdatesMock.mockReturnValue({ query: { data: undefined } });

    render(<DashboardContainer />);
    expect(screen.getByTestId("spinner")).toBeInTheDocument();
  });

  it("renders dashboard with enriched update flags", () => {
    useDashboardDataMock.mockReturnValue({
      overview: {
        attentionNodes: [{ id: "n1" }],
        healthyNodes: [{ id: "n2" }],
      },
      timeline: { points: [] },
      agentVersions: { n1: "0.9.0", n2: "1.0.0" },
      isLoading: false,
    });
    useDiscoveredClientsMock.mockReturnValue({
      discoveredClients: [
        { status: "pending_review" },
        { status: "adopted" },
      ],
    });
    useUpdatesMock.mockReturnValue({
      query: { data: { state: { latest_agent_version: "1.0.0" } } },
    });

    render(<DashboardContainer />);
    expect(screen.getByTestId("attention")).toHaveTextContent("1");
    expect(screen.getByTestId("healthy")).toHaveTextContent("1");
    expect(screen.getByTestId("pending")).toHaveTextContent("1");
    // n1's 0.9.0 mismatches latest 1.0.0 -> updateAvailable true.
    expect(screen.getByTestId("has-update")).toHaveTextContent("yes");
  });
});
