import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const navigateSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
}));

vi.mock("@/ui", () => ({
  Spinner: () => <div data-testid="spinner" />,
  EmptyState: (props: { title: string; action?: React.ReactNode }) => (
    <div data-testid="empty-state">
      <span>{props.title}</span>
      {props.action}
    </div>
  ),
  Button: (props: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props} />
  ),
  SkeletonRows: ({
    count,
    label = "Загрузка списка…",
  }: {
    count: number;
    label?: string;
  }) => (
    <div data-testid="skeleton-rows">
      <output aria-label={label} data-testid="skeleton-row" />
      {Array.from({ length: Math.max(0, count - 1) }).map((_, i) => (
        <div key={i} data-testid="skeleton-row" />
      ))}
    </div>
  ),
  usePrefersReducedMotion: () => false,
}));

vi.mock("@/features/servers/ServersPage", () => ({
  ServersPage: (props: {
    servers: { id: string; updateAvailable?: boolean }[];
    onServerClick: (id: string) => void;
  }) => (
    <div>
      <span data-testid="count">{props.servers.length}</span>
      <span data-testid="has-update">
        {props.servers.some((s) => s.updateAvailable) ? "yes" : "no"}
      </span>
      <button onClick={() => props.onServerClick("s99")}>click</button>
    </div>
  ),
}));

vi.mock("@/components/ErrorState", () => ({
  ErrorState: ({ title, description }: { title?: string; description?: string }) => (
    <div data-testid="error">{description ?? title}</div>
  ),
}));

const useServersListMock = vi.fn();
vi.mock("@/features/servers/hooks/useServersList", () => ({
  useServersList: () => useServersListMock(),
}));

vi.mock("@/features/servers/hooks/useFleetGroups", () => ({
  useFleetGroups: () => ({ fleetGroups: [] }),
}));

vi.mock("@/shared/hooks/useViewMode", () => ({
  useViewMode: () => ({
    resolveMode: () => "grid",
    setMode: vi.fn(),
  }),
}));

const useUpdatesMock = vi.fn();
vi.mock("@/shared/hooks/useUpdates", () => ({
  useUpdates: () => useUpdatesMock(),
}));

// The bulk-action wiring inside ServersContainer uses @tanstack/react-query's
// useMutation + apiClient.createJob. The tests in this file don't exercise
// the bulk path, so the mocks stay minimal: a no-op mutation and a stub
// apiClient. This avoids pulling a QueryClientProvider into every render.
vi.mock("@tanstack/react-query", () => ({
  useMutation: () => ({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
    mutate: vi.fn(),
    isPending: false,
  }),
}));

vi.mock("@/shared/api/api", () => ({
  apiClient: {
    createJob: vi.fn(),
  },
}));

import { ServersContainer } from "./ServersContainer";

describe("ServersContainer", () => {
  beforeEach(() => {
    navigateSpy.mockReset();
    useUpdatesMock.mockReturnValue({ query: { data: undefined } });
  });

  it("shows skeleton placeholders while loading (2.5)", () => {
    useServersListMock.mockReturnValue({
      servers: [],
      agentVersions: {},
      isLoading: true,
      error: null,
    });

    render(<ServersContainer />);
    expect(screen.getByRole("status", { name: /Загрузка/ })).toBeInTheDocument();
  });

  it("renders ErrorState when fetch fails", () => {
    useServersListMock.mockReturnValue({
      servers: [],
      agentVersions: {},
      isLoading: false,
      error: new Error("boom"),
    });

    render(<ServersContainer />);
    expect(screen.getByTestId("error")).toHaveTextContent("boom");
  });

  it("enriches servers with updateAvailable when agent version mismatches latest", () => {
    useServersListMock.mockReturnValue({
      servers: [
        { id: "s1", name: "edge-1" },
        { id: "s2", name: "edge-2" },
      ],
      agentVersions: { s1: "0.9.0", s2: "1.0.0" },
      isLoading: false,
      error: null,
    });
    useUpdatesMock.mockReturnValue({
      query: { data: { state: { latest_agent_version: "1.0.0" } } },
    });

    render(<ServersContainer />);
    expect(screen.getByTestId("has-update")).toHaveTextContent("yes");
  });

  it("navigates to server detail on click", () => {
    useServersListMock.mockReturnValue({
      servers: [{ id: "s1" }],
      agentVersions: {},
      isLoading: false,
      error: null,
    });

    render(<ServersContainer />);
    screen.getByText("click").click();
    expect(navigateSpy).toHaveBeenCalledWith({
      to: "/servers/$serverId",
      params: { serverId: "s99" },
    });
  });
});
