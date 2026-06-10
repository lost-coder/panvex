import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { TokenFooter } from "./TokenFooter";

describe("TokenFooter", () => {
  it("renders the remaining minutes from remainingSecs", () => {
    render(<TokenFooter tokenValue="tok_0123456789abcdef" remainingSecs={620} />);
    // ceil(620 / 60) = 11
    expect(screen.getByText("11 min")).toBeInTheDocument();
    expect(screen.getByText(/tok_012345678/)).toBeInTheDocument();
  });

  it("shows the expired state with a regenerate action when remainingSecs <= 0", async () => {
    const onRegenerate = vi.fn();
    render(
      <TokenFooter tokenValue="tok_0123456789abcdef" remainingSecs={0} onRegenerate={onRegenerate} />,
    );
    expect(screen.getByText("Token expired")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "New token" }));
    expect(onRegenerate).toHaveBeenCalledTimes(1);
  });

  it("omits the regenerate button when no handler is provided", () => {
    render(<TokenFooter tokenValue="tok_x" remainingSecs={0} />);
    expect(screen.getByText("Token expired")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
