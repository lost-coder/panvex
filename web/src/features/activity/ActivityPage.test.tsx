import { fireEvent, render, screen } from "@testing-library/react";
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

  // R10b Task 2: job status "expired" already renders in the table (a
  // job whose TTL lapsed before every target confirmed) but had no
  // filter chip — operators couldn't isolate expired jobs from the
  // rest of the list.
  it("renders an 'expired' filter chip and filters the job list to only expired jobs", () => {
    const props = makeProps({
      jobs: [
        {
          id: "job-1",
          action: "succeeded_marker_action",
          status: "succeeded",
          actorId: "u-1",
          targetCount: 1,
          createdAtUnix: Date.now() / 1000,
        },
        {
          id: "job-2",
          action: "expired_marker_action",
          status: "expired",
          actorId: "u-1",
          targetCount: 1,
          createdAtUnix: Date.now() / 1000,
        },
      ],
    });
    render(<ActivityPage {...props} />);

    // Both rows visible before filtering (action text with underscores
    // replaced by spaces — see prettyAction).
    expect(screen.getByText("succeeded marker action")).toBeInTheDocument();
    expect(screen.getByText("expired marker action")).toBeInTheDocument();

    // The chip's accessible name is its label text plus an adjoining
    // count span ("expired" + "1" with no separator) — getByRole with a
    // regex matches on the substring instead of requiring exact text,
    // and disambiguates it from the row's plain-text status label.
    const chip = screen.getByRole("button", { name: /expired/i });
    fireEvent.click(chip);

    expect(screen.getByText("expired marker action")).toBeInTheDocument();
    expect(screen.queryByText("succeeded marker action")).toBeNull();
  });
});
