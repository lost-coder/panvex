import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ProfilePageProps } from "@/shared/api/types-pages/profile";
import { ProfilePage } from "./ProfilePage";

// R-Q-13: ProfilePage smoke-test.

function makeProps(overrides: Partial<ProfilePageProps> = {}): ProfilePageProps {
  return {
    user: {
      id: "u-1",
      username: "operator-a",
      role: "operator",
      totpEnabled: false,
    },
    appearance: {
      theme: "system",
      density: "comfortable",
      helpMode: "basic",
      swipeNavigation: true,
    },
    ...overrides,
  };
}

describe("ProfilePage", () => {
  it("renders the username", () => {
    render(<ProfilePage {...makeProps({ user: { id: "u-2", username: "renders-this", role: "admin", totpEnabled: true } })} />);
    expect(screen.getAllByText("renders-this").length).toBeGreaterThan(0);
  });

  it("does not throw on minimal props", () => {
    expect(() => render(<ProfilePage {...makeProps()} />)).not.toThrow();
  });
});
