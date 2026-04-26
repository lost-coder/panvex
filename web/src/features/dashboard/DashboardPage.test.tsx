import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { DashboardPageProps } from "@/shared/api/types-pages/dashboard";
import { DashboardPage } from "./DashboardPage";

// R-Q-13: DashboardPage smoke-test.

function makeProps(overrides: Partial<DashboardPageProps> = {}): DashboardPageProps {
  return {
    overview: {
      kpis: [],
      trends: [],
      alerts: [],
      attentionNodes: [],
      healthyNodes: [],
    },
    timeline: { events: [] },
    ...overrides,
  };
}

describe("DashboardPage", () => {
  it("renders without throwing on empty overview", () => {
    expect(() => render(<DashboardPage {...makeProps()} />)).not.toThrow();
  });

  it("renders sections in the document", () => {
    const { container } = render(<DashboardPage {...makeProps()} />);
    expect(container.querySelectorAll("section").length).toBeGreaterThan(0);
  });
});
