import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { EnrollmentTokensPageProps } from "@/shared/api/types-pages/enrollment";
import { EnrollmentTokensPage } from "./EnrollmentTokensPage";

// R-Q-13: EnrollmentTokensPage smoke-test.

function makeProps(overrides: Partial<EnrollmentTokensPageProps> = {}): EnrollmentTokensPageProps {
  return {
    tokens: [],
    onCreateToken: vi.fn(),
    onRevoke: vi.fn(),
    ...overrides,
  };
}

describe("EnrollmentTokensPage", () => {
  it("renders without throwing on empty token list", () => {
    expect(() => render(<EnrollmentTokensPage {...makeProps()} />)).not.toThrow();
  });

  it("renders without throwing when tokens are supplied", () => {
    const now = Math.floor(Date.now() / 1000);
    const props = makeProps({
      tokens: [
        {
          handle: "abcd1234abcd1234",
          maskedValue: "tok-XX…",
          fleetGroupId: "fg-1",
          status: "active",
          issuedAtUnix: now,
          expiresAtUnix: now + 3600,
        },
      ],
    });
    expect(() => render(<EnrollmentTokensPage {...props} />)).not.toThrow();
  });
});
