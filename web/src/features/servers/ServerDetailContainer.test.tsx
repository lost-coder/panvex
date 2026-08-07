import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// GET/PUT .../telemt/update-strategy is admin-only server-side (routes.go).
// This file exercises ONLY the admin gating around TelemtUpdateSection —
// every other dependency the container pulls in is mocked to a minimal
// stand-in so the test stays focused and doesn't need a QueryClient/router.

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  useParams: () => ({ serverId: "agent-1" }),
}));

vi.mock("@/ui", () => ({
  SkeletonRows: () => <div data-testid="skeleton" />,
}));

vi.mock("@/components/ErrorState", () => ({
  ErrorState: ({ description }: { description?: string }) => (
    <div data-testid="error">{description}</div>
  ),
}));

const useServerDetailMock = vi.fn();
vi.mock("./hooks/useServerDetail", () => ({
  useServerDetail: () => useServerDetailMock(),
}));

vi.mock("./hooks/useServerMutations", () => ({
  useServerMutations: () => ({
    allowCertRecoveryMutation: { mutate: vi.fn() },
    revokeCertRecoveryMutation: { mutate: vi.fn() },
    boostDetailMutation: { mutate: vi.fn() },
    renameMutation: { mutate: vi.fn() },
    updateFleetGroupMutation: { mutate: vi.fn() },
    deregisterMutation: { mutate: vi.fn() },
  }),
}));

vi.mock("./hooks/useServerHistory", () => ({
  useServerLoadHistory: () => ({ points: [], resolution: "raw" }),
}));

vi.mock("./hooks/useFleetGroups", () => ({
  useFleetGroups: () => ({ fleetGroups: [] }),
}));

vi.mock("./enrollment/EnrollmentHistory", () => ({
  EnrollmentHistory: () => <div data-testid="enrollment-history" />,
}));

vi.mock("./runtime-events/RuntimeEvents", () => ({
  RuntimeEvents: () => <div data-testid="runtime-events" />,
}));

// The component under test for the gating behaviour: mounting it is what
// would fire the (admin-only) GET .../telemt/update-strategy request, so
// the spy doubles as a "fetch was never attempted" proxy for a non-admin.
const telemtUpdateSectionSpy = vi.fn();
vi.mock("./server-detail/TelemtUpdateSection", () => ({
  TelemtUpdateSection: (props: { agentId: string; currentVersion: string; latestVersion?: string }) => {
    telemtUpdateSectionSpy(props);
    return <div data-testid="telemt-update-section" />;
  },
}));

vi.mock("@/shared/hooks/useUpdates", () => ({
  useUpdates: () => ({ query: { data: undefined } }),
}));

vi.mock("@/shared/api/api", () => ({
  apiClient: { updateAgent: vi.fn() },
}));

vi.mock("@/shared/api/transforms/servers", () => ({
  transformAgentConnection: () => undefined,
}));

vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => vi.fn().mockResolvedValue(true),
}));

const useProfileMock = vi.fn();
vi.mock("@/features/auth/hooks/useProfile", () => ({
  useProfile: () => useProfileMock(),
}));

vi.mock("./ServerDetailPage", () => ({
  ServerDetailPage: (props: { telemtUpdateSlot?: React.ReactNode }) => (
    <div data-testid="server-detail-page">{props.telemtUpdateSlot}</div>
  ),
}));

import { ServerDetailContainer } from "./ServerDetailContainer";

describe("ServerDetailContainer — telemt update strategy admin gating", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useServerDetailMock.mockReturnValue({
      server: { id: "agent-1", name: "edge-1" },
      initState: undefined,
      lastUpdatedAt: undefined,
      raw: undefined,
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });
  });

  it("does not render (or mount the data hook of) the section for a non-admin", () => {
    useProfileMock.mockReturnValue({ profile: { role: "operator" } });
    render(<ServerDetailContainer />);
    expect(screen.queryByTestId("telemt-update-section")).not.toBeInTheDocument();
    expect(telemtUpdateSectionSpy).not.toHaveBeenCalled();
  });

  it("does not render the section while the profile is still loading (role unknown)", () => {
    useProfileMock.mockReturnValue({ profile: undefined });
    render(<ServerDetailContainer />);
    expect(screen.queryByTestId("telemt-update-section")).not.toBeInTheDocument();
    expect(telemtUpdateSectionSpy).not.toHaveBeenCalled();
  });

  it("renders the section for an admin", () => {
    useProfileMock.mockReturnValue({ profile: { role: "admin" } });
    render(<ServerDetailContainer />);
    expect(screen.getByTestId("telemt-update-section")).toBeInTheDocument();
    expect(telemtUpdateSectionSpy).toHaveBeenCalledWith({
      agentId: "agent-1",
      currentVersion: "",
      latestVersion: undefined,
      availableVersions: [],
    });
  });
});
