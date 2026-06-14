import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { DriftBadge } from "./DriftBadge";

// i18next is bootstrapped globally in vitest.setup.ts (initI18n), so
// useTranslation("servers") resolves the real config.drift.* labels
// from src/locales/en/servers.json without a per-test provider wrapper.
describe("DriftBadge", () => {
  it("renders the in-sync label", () => {
    render(<DriftBadge status="in_sync" />);
    expect(screen.getByText("In sync")).toBeInTheDocument();
  });

  it("renders the drifted label", () => {
    render(<DriftBadge status="drifted" />);
    expect(screen.getByText("Drifted")).toBeInTheDocument();
  });

  it("renders the unknown label", () => {
    render(<DriftBadge status="unknown" />);
    expect(screen.getByText("Unknown")).toBeInTheDocument();
  });
});
