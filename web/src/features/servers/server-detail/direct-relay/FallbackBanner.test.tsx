import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

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
});
