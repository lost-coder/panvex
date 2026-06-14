import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// useUnsavedChangesGuard (audit E4) calls useBlocker + useConfirm.
// Mock both so the page renders without a Router or ConfirmProvider.
vi.mock("@tanstack/react-router", () => ({ useBlocker: vi.fn() }));
vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => vi.fn().mockResolvedValue(true),
}));

import type { SettingsPageProps } from "@/shared/api/types-pages/settings";
import { SettingsPage } from "./SettingsPage";

// R-Q-13: SettingsPage smoke-test.

function makeProps(overrides: Partial<SettingsPageProps> = {}): SettingsPageProps {
  return {
    panelSettings: {
      httpPublicUrl: "http://localhost:8080",
      grpcPublicEndpoint: "localhost:8443",
      passwordMinLength: 12,
    },
    appearanceSettings: {
      theme: "system",
      density: "comfortable",
      helpMode: "basic",
      swipeNavigation: false,
    },
    ...overrides,
  };
}

describe("SettingsPage", () => {
  it("renders without throwing on minimal props", () => {
    expect(() => render(<SettingsPage {...makeProps()} />)).not.toThrow();
  });

  it("renders sections in the document", () => {
    const { container } = render(<SettingsPage {...makeProps()} />);
    expect(container.querySelectorAll("section").length).toBeGreaterThan(0);
  });
});
