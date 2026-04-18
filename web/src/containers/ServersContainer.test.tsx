import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const navigateSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
}));

vi.mock("@lost-coder/panvex-ui", () => ({
  Spinner: () => <div data-testid="spinner" />,
}));

vi.mock("@/pages/ServersPage", () => ({
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
  ErrorState: ({ message }: { message: string }) => (
    <div data-testid="error">{message}</div>
  ),
}));

const useServersListMock = vi.fn();
vi.mock("@/hooks/useServersList", () => ({
  useServersList: () => useServersListMock(),
}));

vi.mock("@/hooks/useFleetGroups", () => ({
  useFleetGroups: () => ({ fleetGroups: [] }),
}));

vi.mock("@/hooks/useViewMode", () => ({
  useViewMode: () => ({
    resolveMode: () => "grid",
    setMode: vi.fn(),
  }),
}));

const useUpdatesMock = vi.fn();
vi.mock("@/hooks/useUpdates", () => ({
  useUpdates: () => useUpdatesMock(),
}));

import { ServersContainer } from "./ServersContainer";

describe("ServersContainer", () => {
  beforeEach(() => {
    navigateSpy.mockReset();
    useUpdatesMock.mockReturnValue({ query: { data: undefined } });
  });

  it("shows spinner while loading", () => {
    useServersListMock.mockReturnValue({
      servers: [],
      agentVersions: {},
      isLoading: true,
      error: null,
    });

    render(<ServersContainer />);
    expect(screen.getByTestId("spinner")).toBeInTheDocument();
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
