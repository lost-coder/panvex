import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Mock every hook/child the container touches so we're testing the
// container's wiring, not the transitive dependency graph. The UI-kit
// page is stubbed to a pair of divs so we can read back the props that
// flowed through.
const navigateSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
}));

vi.mock("@lost-coder/panvex-ui", () => ({
  Spinner: () => <div data-testid="spinner" />,
}));

vi.mock("@/pages/ClientsPage", () => ({
  ClientsPage: (props: {
    clients: { id: string; name: string }[];
    pendingDiscoveredCount: number;
    onClientClick: (id: string) => void;
  }) => (
    <div>
      <span data-testid="count">{props.clients.length}</span>
      <span data-testid="pending">{props.pendingDiscoveredCount}</span>
      <button onClick={() => props.onClientClick("c42")}>click</button>
    </div>
  ),
}));

const useClientsListMock = vi.fn();
vi.mock("@/hooks/useClientsList", () => ({
  useClientsList: () => useClientsListMock(),
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

vi.mock("@/hooks/useViewMode", () => ({
  useViewMode: () => ({
    resolveMode: () => "grid",
    setMode: vi.fn(),
  }),
}));

import { ClientsContainer } from "./ClientsContainer";

describe("ClientsContainer", () => {
  beforeEach(() => {
    navigateSpy.mockReset();
  });

  it("renders spinner while loading", () => {
    useClientsListMock.mockReturnValue({ clients: [], isLoading: true });
    useDiscoveredClientsMock.mockReturnValue({ discoveredClients: [] });

    render(<ClientsContainer />);
    expect(screen.getByTestId("spinner")).toBeInTheDocument();
  });

  it("passes clients + pending count through to ClientsPage", () => {
    useClientsListMock.mockReturnValue({
      clients: [
        { id: "c1", name: "alpha" },
        { id: "c2", name: "beta" },
      ],
      isLoading: false,
    });
    useDiscoveredClientsMock.mockReturnValue({
      discoveredClients: [
        { status: "pending_review" },
        { status: "adopted" },
        { status: "pending_review" },
      ],
    });

    render(<ClientsContainer />);
    expect(screen.getByTestId("count")).toHaveTextContent("2");
    expect(screen.getByTestId("pending")).toHaveTextContent("2");
  });

  it("navigates to /clients/$id on client click", async () => {
    useClientsListMock.mockReturnValue({
      clients: [{ id: "c1" }],
      isLoading: false,
    });
    useDiscoveredClientsMock.mockReturnValue({ discoveredClients: [] });

    render(<ClientsContainer />);
    screen.getByText("click").click();
    expect(navigateSpy).toHaveBeenCalledWith({
      to: "/clients/$clientId",
      params: { clientId: "c42" },
    });
  });
});
