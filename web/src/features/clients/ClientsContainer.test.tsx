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

// Bulk-action wiring uses useMutation + useQueryClient + apiClient. The
// tests in this file don't exercise the bulk path, so we stub them here
// to avoid pulling a QueryClientProvider into every render.
vi.mock("@tanstack/react-query", () => ({
  useMutation: () => ({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
    mutate: vi.fn(),
    isPending: false,
  }),
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
    getQueryData: vi.fn(),
  }),
}));

vi.mock("@/shared/api/api", () => ({
  apiClient: {
    client: vi.fn(),
    updateClient: vi.fn(),
    deleteClient: vi.fn(),
  },
}));

vi.mock("@/shared/api/transforms/clients", () => ({
  buildClientInput: vi.fn(),
}));

vi.mock("@/ui", () => ({
  Spinner: () => <div data-testid="spinner" />,
  EmptyState: (props: { title: string; description?: string }) => (
    <div data-testid="empty-state">
      <span>{props.title}</span>
      {props.description ? <span>{props.description}</span> : null}
    </div>
  ),
  usePrefersReducedMotion: () => false,
}));

vi.mock("@/features/clients/ClientsPage", () => ({
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
vi.mock("@/features/clients/hooks/useClientsList", () => ({
  useClientsList: () => useClientsListMock(),
}));

const useDiscoveredClientsMock = vi.fn();
vi.mock("@/features/clients/hooks/useDiscoveredClients", () => ({
  useDiscoveredClients: () => useDiscoveredClientsMock(),
}));

vi.mock("@/features/clients/hooks/useClientCreate", () => ({
  useClientCreate: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
    error: null,
  }),
}));

vi.mock("@/shared/hooks/useViewMode", () => ({
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

  it("renders skeleton rows while loading (2.5)", () => {
    useClientsListMock.mockReturnValue({ clients: [], isLoading: true });
    useDiscoveredClientsMock.mockReturnValue({ discoveredClients: [] });

    render(<ClientsContainer />);
    // The first skeleton row carries role=status + Russian-locale label
    // so assistive tech announces a single "loading list" message.
    expect(screen.getByRole("status", { name: /Загрузка/ })).toBeInTheDocument();
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
