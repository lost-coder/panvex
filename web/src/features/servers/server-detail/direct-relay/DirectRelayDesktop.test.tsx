import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { mockDirectServer } from "@/test/fixtures";

import { DirectRelayDesktop } from "./DirectRelayDesktop";

describe("DirectRelayDesktop", () => {
  it("renders upstreams list and health card, hides DC tiles", () => {
    render(
      <DirectRelayDesktop
        server={mockDirectServer()}
        initState={undefined}
        alertItems={[]}
        metricsChart={undefined}
        fallback={{ active: false, durationSeconds: 0, escalated: false, enteredAtUnix: null }}
      />,
    );
    expect(screen.getByText(/Upstream health/i)).toBeInTheDocument();
    expect(screen.queryByText(/Data Centers/i)).not.toBeInTheDocument();
  });
  it("renders FallbackBanner only when fallback flag set", () => {
    render(
      <DirectRelayDesktop
        server={mockDirectServer()}
        initState={undefined}
        alertItems={[]}
        metricsChart={undefined}
        fallback={{ active: true, durationSeconds: 300, escalated: false, enteredAtUnix: 1_700_000_000 }}
      />,
    );
    expect(screen.getByText(/running on direct fallback/i)).toBeInTheDocument();
  });
});
