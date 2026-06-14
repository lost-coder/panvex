import { render } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

// The page now embeds GroupConfigSection, which pulls in the global toast
// channel and fetches the group config via useGroupConfig. Mock both so the
// page renders without a ToastProvider, QueryClient, or network (the section
// has its own dedicated test).
vi.mock("@/app/providers/ToastProvider", () => ({
  useToast: () => ({
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    withAction: vi.fn(),
    dismiss: vi.fn(),
  }),
}));
// useUnsavedChangesGuard (audit E4) calls useBlocker + useConfirm.
// Mock both so the page renders without a Router or ConfirmProvider.
vi.mock("@tanstack/react-router", () => ({
  useBlocker: vi.fn(),
  useNavigate: () => vi.fn(),
}));

vi.mock("@/features/servers/hooks/useServersList", () => ({
  useServersList: () => ({ servers: [], agentVersions: {}, isLoading: false, error: null }),
}));
vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => vi.fn().mockResolvedValue(true),
}));
vi.mock("@/features/servers/config/configHooks", () => ({
  useGroupConfig: () => ({
    data: { sections: {}, nodes: [] },
    isLoading: false,
    isError: false,
  }),
  usePutGroupConfig: () => ({ mutate: vi.fn(), isPending: false }),
  useApplyGroupConfig: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

import { FleetGroupDetailPage } from "./FleetGroupDetailPage";

// R-Q-13: FleetGroupDetailPage smoke-test.

type FleetGroupDetailPageProps = ComponentProps<typeof FleetGroupDetailPage>;

function makeProps(overrides: Partial<FleetGroupDetailPageProps> = {}): FleetGroupDetailPageProps {
  return {
    group: {
      id: "fg-1",
      name: "default",
      label: "Default",
      description: "primary",
      agent_count: 1,
      created_at_unix: 0,
      updated_at_unix: 0,
      integrations: [],
    },
    onBack: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    editOpen: false,
    formData: { name: "", label: "", description: "" },
    formError: "",
    onFormDataChange: vi.fn(),
    onSubmitEdit: vi.fn(),
    onCancelEdit: vi.fn(),
    saving: false,
    ...overrides,
  };
}

describe("FleetGroupDetailPage", () => {
  it("renders the fleet group label", () => {
    const { container } = render(<FleetGroupDetailPage {...makeProps()} />);
    expect(container.textContent).toContain("Default");
  });

  it("renders without throwing on minimal props", () => {
    expect(() => render(<FleetGroupDetailPage {...makeProps()} />)).not.toThrow();
  });
});
