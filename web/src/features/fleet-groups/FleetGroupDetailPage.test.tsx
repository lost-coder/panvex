import { render } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

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
