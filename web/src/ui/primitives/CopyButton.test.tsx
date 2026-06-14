import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { CopyButton } from "./CopyButton";

describe("CopyButton", () => {
  it("exposes a localized accessible name", () => {
    render(<CopyButton text="secret" />);
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
  });

  it("announces the copy through a polite live region", async () => {
    render(<CopyButton text="secret" />);
    await userEvent.click(screen.getByRole("button", { name: "Copy" }));
    expect(screen.getByRole("status")).toHaveTextContent("Copied");
  });
});
