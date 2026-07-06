import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/ui", () => ({
  SkeletonRows: () => <div data-testid="skeleton-rows" />,
}));

vi.mock("./UsersManagementPage", () => ({
  UsersManagementPage: (props: { users: { id: string }[] }) => (
    <div data-testid="users-page">{props.users.length}</div>
  ),
}));

vi.mock("@/components/ErrorState", () => ({
  ErrorState: ({ title, description }: { title?: string; description?: string }) => (
    <div data-testid="error">{description ?? title}</div>
  ),
}));

const useUsersMock = vi.fn();
vi.mock("./hooks/useUsers", () => ({
  useUsers: () => useUsersMock(),
}));

vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => vi.fn().mockResolvedValue(false),
}));

import { UsersContainer } from "./UsersContainer";

const mutationStub = { mutate: vi.fn(), isPending: false };

describe("UsersContainer", () => {
  beforeEach(() => {
    useUsersMock.mockReset();
  });

  it("renders ErrorState (not an empty list) when the query fails", () => {
    useUsersMock.mockReturnValue({
      users: [],
      isLoading: false,
      error: new Error("boom"),
      refetch: vi.fn(),
      createUser: mutationStub,
      updateUser: mutationStub,
      deleteUser: mutationStub,
      resetTotp: mutationStub,
    });
    render(<UsersContainer />);
    expect(screen.getByTestId("error")).toHaveTextContent("boom");
    expect(screen.queryByTestId("users-page")).not.toBeInTheDocument();
  });
});
