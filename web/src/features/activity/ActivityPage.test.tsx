import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ActivityPageProps } from "@/shared/api/types-pages/activity";
import { ActivityPage } from "./ActivityPage";

// R-Q-13: ActivityPage smoke-test.

function makeProps(overrides: Partial<ActivityPageProps> = {}): ActivityPageProps {
  return {
    jobs: [],
    auditEvents: [],
    activeTab: "jobs",
    onTabChange: vi.fn(),
    ...overrides,
  };
}

describe("ActivityPage", () => {
  it("renders without throwing on empty lists", () => {
    expect(() => render(<ActivityPage {...makeProps()} />)).not.toThrow();
  });

  it("renders without throwing when at least one job is present", () => {
    const props = makeProps({
      jobs: [
        {
          id: "job-1",
          action: "client.create",
          status: "succeeded",
          actorId: "u-1",
          targetCount: 1,
          createdAtUnix: Date.now() / 1000,
        },
      ],
    });
    expect(() => render(<ActivityPage {...props} />)).not.toThrow();
  });
});
