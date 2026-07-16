import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { UserTelemetrySuppressedBanner } from "./UserTelemetrySuppressedBanner";

describe("UserTelemetrySuppressedBanner", () => {
  it("warns that per-user telemetry is suppressed and the figures are stale", () => {
    render(<UserTelemetrySuppressedBanner />);

    // role="status", not "alert": this is a warning about telemetry fidelity,
    // not a node outage, so it must not preempt assistive tech.
    const banner = screen.getByRole("status");
    expect(banner).toHaveTextContent(/per-user traffic telemetry is suppressed/i);
    expect(banner).toHaveTextContent(/stale/i);
  });
});
