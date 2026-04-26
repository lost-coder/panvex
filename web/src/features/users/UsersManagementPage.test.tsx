import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { UsersManagementPageProps } from "@/shared/api/types-pages/users";
import { UsersManagementPage } from "./UsersManagementPage";

// R-Q-13: UsersManagementPage smoke-test.

function makeProps(overrides: Partial<UsersManagementPageProps> = {}): UsersManagementPageProps {
  return {
    users: [],
    onAdd: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    onResetTotp: vi.fn(),
    ...overrides,
  };
}

describe("UsersManagementPage", () => {
  it("renders without throwing on empty user list", () => {
    expect(() => render(<UsersManagementPage {...makeProps()} />)).not.toThrow();
  });

  it("renders rows when users are supplied", () => {
    const props = makeProps({
      users: [
        {
          id: "u-1",
          username: "alice",
          role: "admin",
          totpEnabled: true,
          createdAt: new Date().toISOString(),
        },
      ],
    });
    const { container } = render(<UsersManagementPage {...props} />);
    expect(container.textContent).toContain("alice");
  });
});
