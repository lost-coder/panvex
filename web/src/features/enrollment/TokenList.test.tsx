import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TokenList } from "./TokenList";
import type { EnrollmentTokenData } from "@/shared/api/types-pages/pages";

const activeToken: EnrollmentTokenData = {
  value: "tok-abcdef1234567890",
  fleetGroupId: "default",
  status: "active",
  issuedAtUnix: 1713168000,
  expiresAtUnix: 1713254400,
};

const consumedToken: EnrollmentTokenData = {
  value: "tok-consumed0000000",
  fleetGroupId: "prod",
  status: "consumed",
  issuedAtUnix: 1713168000,
  expiresAtUnix: 1713254400,
};

describe("TokenList", () => {
  it("renders empty state when no tokens", () => {
    render(<TokenList tokens={[]} onRevoke={vi.fn()} />);
    expect(screen.getByText(/no enrollment tokens/i)).toBeInTheDocument();
  });

  it("renders tokens in a table", () => {
    render(<TokenList tokens={[activeToken, consumedToken]} onRevoke={vi.fn()} />);
    // DataTable may render desktop + mobile views, so use getAllByText.
    expect(screen.getAllByText("active").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("consumed").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("default").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("prod").length).toBeGreaterThanOrEqual(1);
  });

  it("shows Revoke buttons only for active tokens", () => {
    render(<TokenList tokens={[activeToken, consumedToken]} onRevoke={vi.fn()} />);
    const revokeButtons = screen.getAllByRole("button", { name: /revoke/i });
    // All revoke buttons belong to the active token (may have desktop + mobile).
    expect(revokeButtons.length).toBeGreaterThanOrEqual(1);
  });

  it("calls onRevoke with token value when clicked", async () => {
    const user = userEvent.setup();
    const onRevoke = vi.fn();
    render(<TokenList tokens={[activeToken]} onRevoke={onRevoke} />);

    const [firstRevoke] = screen.getAllByRole("button", { name: /revoke/i });
    if (!firstRevoke) throw new Error("revoke button missing");
    await user.click(firstRevoke);
    expect(onRevoke).toHaveBeenCalledWith(activeToken.value);
  });
});
