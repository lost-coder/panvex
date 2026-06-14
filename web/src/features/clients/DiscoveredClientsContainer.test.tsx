// Tests for DiscoveredClientsContainer — wiring only. Mirrors the
// ClientsContainer.test.tsx approach: mock the hook and the child page,
// then assert that props flow through correctly.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const navigateSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
}));

vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => vi.fn().mockResolvedValue(true),
}));

vi.mock("@/shared/hooks/useUrlSearchState", () => ({
  useUrlSearchState: () => ["", vi.fn()],
}));

vi.mock("@/features/clients/lib/groupDiscovered", () => ({
  groupDiscovered: () => [],
}));

vi.mock("@/ui", () => ({
  SkeletonRows: ({ count, label }: { count: number; label?: string }) => (
    <div data-testid="skeleton-rows">
      <output aria-label={label} data-testid="skeleton-row" />
      {Array.from({ length: Math.max(0, count - 1) }, (_, i) => `row-${i}`).map((id) => (
        <div key={id} data-testid="skeleton-row" />
      ))}
    </div>
  ),
}));

vi.mock("@/components/ErrorState", () => ({
  ErrorState: ({ description }: { description: string }) => (
    <div data-testid="error-state">{description}</div>
  ),
}));

// Stub DiscoveredClientsPage: render a button wired to onRescan so we
// can assert the prop was wired correctly.
vi.mock("@/features/clients/DiscoveredClientsPage", () => ({
  DiscoveredClientsPage: (props: {
    onRescan?: () => void;
    rescanning?: boolean;
  }) => (
    <div>
      <button
        data-testid="rescan-btn"
        aria-label="rescan"
        disabled={props.rescanning}
        onClick={props.onRescan}
      />
    </div>
  ),
}));

// Mock the hook directly — the hook's internals are tested separately.
const rescanSpy = vi.fn().mockResolvedValue(undefined);
const useDiscoveredClientsMock = vi.fn();
vi.mock("@/features/clients/hooks/useDiscoveredClients", () => ({
  useDiscoveredClients: () => useDiscoveredClientsMock(),
}));

import { DiscoveredClientsContainer } from "./DiscoveredClientsContainer";

const defaultHookReturn = {
  discoveredClients: [],
  groupCounts: { all: 0, pending: 0, adopted: 0, ignored: 0, conflicts: 0 },
  isLoading: false,
  error: null,
  adopt: vi.fn(),
  ignore: vi.fn(),
  adoptMany: vi.fn(),
  ignoreMany: vi.fn(),
  rescan: rescanSpy,
  isAdopting: false,
  isIgnoring: false,
  isRescanning: false,
};

describe("DiscoveredClientsContainer", () => {
  beforeEach(() => {
    rescanSpy.mockClear();
    useDiscoveredClientsMock.mockReturnValue(defaultHookReturn);
  });

  it("renders without throwing", () => {
    expect(() => render(<DiscoveredClientsContainer />)).not.toThrow();
  });

  it("calls rescan() when the rescan button is clicked", async () => {
    render(<DiscoveredClientsContainer />);
    const btn = screen.getByTestId("rescan-btn");
    await userEvent.click(btn);
    expect(rescanSpy).toHaveBeenCalledTimes(1);
  });

  it("passes rescanning=true to DiscoveredClientsPage while isRescanning", () => {
    useDiscoveredClientsMock.mockReturnValue({
      ...defaultHookReturn,
      isRescanning: true,
    });
    render(<DiscoveredClientsContainer />);
    expect(screen.getByTestId("rescan-btn")).toBeDisabled();
  });
});
