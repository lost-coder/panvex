import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { EnrollmentAttemptDetail } from "@/shared/api/types-enrollment";

import { EnrollmentTimeline } from "./EnrollmentTimeline";

// vitest.setup.ts initialises i18next with the project's default
// language (English), so assertions match the EN translation that the
// component will resolve at render time. If you flip the default back
// to Russian (or any other locale), update the expected substrings
// here too — the test still covers the same lookup path.

const baseFixture: EnrollmentAttemptDetail = {
  attempt: {
    id: "att-1",
    token_id: "tok-1",
    mode: "inbound",
    request_id: "rid-1",
    status: "in_progress",
    started_at: "2026-05-13T12:00:00Z",
  },
  events: [
    {
      step: "bootstrap_request_received",
      level: "info",
      message: "received",
      ts: "2026-05-13T12:00:00Z",
    },
    {
      step: "token_validated",
      level: "info",
      message: "ok",
      ts: "2026-05-13T12:00:01Z",
    },
  ],
};

describe("EnrollmentTimeline", () => {
  it("renders each known step using the i18n step label", () => {
    render(<EnrollmentTimeline detail={baseFixture} />);
    expect(
      screen.getByText(/Bootstrap request received/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Token validated/)).toBeInTheDocument();
  });

  it("falls back to the raw step key for unknown labels", () => {
    const detail: EnrollmentAttemptDetail = {
      ...baseFixture,
      events: [
        {
          step: "future_unknown_step",
          level: "info",
          ts: "2026-05-13T12:00:02Z",
        },
      ],
    };
    render(<EnrollmentTimeline detail={detail} />);
    expect(screen.getByText("future_unknown_step")).toBeInTheDocument();
  });

  it("shows the error banner when the attempt failed", () => {
    const failed: EnrollmentAttemptDetail = {
      ...baseFixture,
      attempt: {
        ...baseFixture.attempt,
        status: "failed",
        error_code: "TOKEN_EXPIRED",
        error_message: "Токен истёк.",
      },
    };
    render(<EnrollmentTimeline detail={failed} />);
    expect(screen.getByText(/Connection failed/)).toBeInTheDocument();
    expect(screen.getByText(/Токен истёк/)).toBeInTheDocument();
    expect(screen.getByText(/TOKEN_EXPIRED/)).toBeInTheDocument();
  });
});
