import { render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";

import { META_SHORTCUTS, NAV_SHORTCUTS } from "@/app/shortcuts";
import { ShortcutsOverlay } from "./ShortcutsOverlay";

// jsdom's <dialog> lacks showModal — stub it so the effect doesn't throw.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
    this.open = true;
  });
  HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
    this.open = false;
  });
});

describe("ShortcutsOverlay", () => {
  it("renders one row per registry entry, including g f", () => {
    render(<ShortcutsOverlay open onOpenChange={vi.fn()} />);
    for (const spec of [...NAV_SHORTCUTS, ...META_SHORTCUTS]) {
      expect(screen.getByText(spec.keys)).toBeInTheDocument();
    }
    expect(screen.getByText("Go to Fleet groups")).toBeInTheDocument();
  });
});
