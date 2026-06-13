import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SubscriptionLinkCard } from "./SubscriptionLinkCard";

// jsdom exposes navigator.clipboard as a getter-only property, so it cannot
// be assigned directly. Install the mock via Object.defineProperty and remove
// it afterwards so tests stay isolated.
let writeText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  // @ts-expect-error remove the mock so it does not leak across files.
  delete navigator.clipboard;
});

describe("SubscriptionLinkCard", () => {
  it("renders the url when non-empty", () => {
    render(
      <SubscriptionLinkCard
        url="https://sub.example.com/abc123"
        rotating={false}
        onRotate={vi.fn()}
      />,
    );
    expect(screen.getByText("https://sub.example.com/abc123")).toBeInTheDocument();
  });

  it("renders the none message when url is empty", () => {
    render(
      <SubscriptionLinkCard url="" rotating={false} onRotate={vi.fn()} />,
    );
    expect(
      screen.getByText(/No public subscription domain/),
    ).toBeInTheDocument();
    // Rotating is pointless with no public domain configured — hide the button.
    expect(
      screen.queryByRole("button", { name: /Rotate/i }),
    ).not.toBeInTheDocument();
  });

  it("clicking Copy calls navigator.clipboard.writeText with the url", () => {
    // Use fireEvent rather than userEvent here: userEvent.setup() installs its
    // own navigator.clipboard stub that would shadow the writeText spy.
    render(
      <SubscriptionLinkCard
        url="https://sub.example.com/abc123"
        rotating={false}
        onRotate={vi.fn()}
      />,
    );

    const copyBtn = screen.getByRole("button", { name: /Copy/i });
    fireEvent.click(copyBtn);

    expect(writeText).toHaveBeenCalledWith("https://sub.example.com/abc123");
  });

  it("clicking Rotate (after confirm) calls onRotate", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const onRotate = vi.fn();
    const user = userEvent.setup();

    render(
      <SubscriptionLinkCard
        url="https://sub.example.com/abc123"
        rotating={false}
        onRotate={onRotate}
      />,
    );

    const rotateBtn = screen.getByRole("button", { name: /Rotate/i });
    await user.click(rotateBtn);

    expect(window.confirm).toHaveBeenCalled();
    expect(onRotate).toHaveBeenCalledOnce();
  });

  it("does not call onRotate when confirm is cancelled", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    const onRotate = vi.fn();
    const user = userEvent.setup();

    render(
      <SubscriptionLinkCard
        url="https://sub.example.com/abc123"
        rotating={false}
        onRotate={onRotate}
      />,
    );

    const rotateBtn = screen.getByRole("button", { name: /Rotate/i });
    await user.click(rotateBtn);

    expect(onRotate).not.toHaveBeenCalled();
  });
});
