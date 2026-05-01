import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";

import { FallbackBanner } from "./FallbackBanner";

describe("FallbackBanner", () => {
  it("warns when escalated=false", () => {
    const { container } = render(
      <FallbackBanner durationSeconds={300} escalated={false} />,
    );
    expect(container.querySelector('[data-severity="warn"]')).toBeInTheDocument();
    expect(screen.getByText(/running on direct fallback/i)).toBeInTheDocument();
  });
  it("renders critical when escalated=true", () => {
    const { container } = render(
      <FallbackBanner durationSeconds={1900} escalated={true} />,
    );
    expect(container.querySelector('[data-severity="critical"]')).toBeInTheDocument();
    expect(screen.getByText(/ME pool down/i)).toBeInTheDocument();
  });

  describe("with enteredAtUnix tick", () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    it("recomputes duration from wall-clock as time advances", () => {
      const enteredAtUnix = 1_700_000_000;
      vi.setSystemTime(new Date(enteredAtUnix * 1000 + 60_000));
      render(
        <FallbackBanner
          durationSeconds={1}
          escalated={false}
          enteredAtUnix={enteredAtUnix}
        />,
      );
      expect(screen.getByText(/Active for 0 min\./i)).toBeInTheDocument();

      act(() => {
        vi.setSystemTime(new Date(enteredAtUnix * 1000 + 270_000));
        vi.advanceTimersByTime(30_000);
      });
      expect(screen.getByText(/Active for 5 min\./i)).toBeInTheDocument();
    });
  });
});
