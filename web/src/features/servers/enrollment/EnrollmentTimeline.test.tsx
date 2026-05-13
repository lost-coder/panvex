import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { EnrollmentAttemptDetail } from "@/shared/api/types-enrollment";

import { EnrollmentTimeline } from "./EnrollmentTimeline";

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
  it("renders each known step with its Russian label", () => {
    render(<EnrollmentTimeline detail={baseFixture} />);
    expect(
      screen.getByText(/Запрос на установку получен/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Токен проверен/)).toBeInTheDocument();
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
    expect(screen.getByText(/Токен истёк/)).toBeInTheDocument();
    expect(screen.getByText(/TOKEN_EXPIRED/)).toBeInTheDocument();
  });
});
