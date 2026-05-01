import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { MeDownHero } from "./MeDownHero";

describe("MeDownHero", () => {
  it("renders critical headline", () => {
    render(<MeDownHero recentEvents={[]} />);
    expect(screen.getByText(/ME pool unavailable/i)).toBeInTheDocument();
  });
});
