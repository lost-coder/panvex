import { render } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import { FleetGroupsPage } from "./FleetGroupsPage";

// R-Q-13: FleetGroupsPage smoke-test.

type FleetGroupsPageProps = ComponentProps<typeof FleetGroupsPage>;

function makeProps(overrides: Partial<FleetGroupsPageProps> = {}): FleetGroupsPageProps {
  return {
    groups: [],
    sheet: { mode: "closed" },
    formData: { name: "", label: "", description: "" },
    formError: "",
    onFormDataChange: vi.fn(),
    onCreate: vi.fn(),
    onEdit: vi.fn(),
    onOpenDetail: vi.fn(),
    onSubmit: vi.fn(),
    onCancel: vi.fn(),
    saving: false,
    ...overrides,
  };
}

describe("FleetGroupsPage", () => {
  it("renders without throwing on empty list", () => {
    expect(() => render(<FleetGroupsPage {...makeProps()} />)).not.toThrow();
  });

  it("renders rows when groups are supplied", () => {
    const props = makeProps({
      groups: [
        {
          id: "fg-1",
          name: "default",
          label: "Default",
          description: "primary fleet",
          agent_count: 2,
          created_at_unix: 0,
          updated_at_unix: 0,
          integrations: [],
        },
      ],
    });
    const { container } = render(<FleetGroupsPage {...props} />);
    expect(container.textContent).toContain("Default");
  });
});
