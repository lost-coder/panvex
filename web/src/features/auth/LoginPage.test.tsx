import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { LoginPageProps } from "@/shared/api/types-pages/login";
import { LoginPage } from "./LoginPage";

// R-Q-13: smoke-test for LoginPage. Renders both stages and verifies the
// page mounts without throwing on minimal props. Catches refactor breaks
// where a sub-component import path or i18n key drifts.

function makeProps(overrides: Partial<LoginPageProps> = {}): LoginPageProps {
  return {
    onCredentials: vi.fn(),
    onTotp: vi.fn(),
    onBack: vi.fn(),
    ...overrides,
  };
}

describe("LoginPage", () => {
  it("renders the credentials stage by default without throwing", () => {
    expect(() => render(<LoginPage {...makeProps()} />)).not.toThrow();
  });

  it("renders the totp stage when stage='totp'", () => {
    const { container } = render(<LoginPage {...makeProps({ stage: "totp" })} />);
    expect(container.querySelector("form")).not.toBeNull();
  });

  it("surfaces the error message when supplied", () => {
    render(<LoginPage {...makeProps({ error: "bad credentials" })} />);
    expect(screen.getByText(/bad credentials/i)).toBeInTheDocument();
  });
});
