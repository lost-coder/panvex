import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const useBlockerSpy = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useBlocker: (opts: unknown) => useBlockerSpy(opts),
}));

const confirmSpy = vi.fn();
vi.mock("@/app/providers/ConfirmProvider", () => ({
  useConfirm: () => confirmSpy,
}));

import { useUnsavedChangesGuard } from "./useUnsavedChangesGuard";

describe("useUnsavedChangesGuard", () => {
  it("does not block when the form is clean", async () => {
    renderHook(() => useUnsavedChangesGuard(false));
    const opts = useBlockerSpy.mock.calls.at(-1)![0] as {
      disabled: boolean;
      shouldBlockFn: () => Promise<boolean>;
    };
    expect(opts.disabled).toBe(true);
    await expect(opts.shouldBlockFn()).resolves.toBe(false);
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it("blocks navigation when dirty and the operator chooses to stay", async () => {
    confirmSpy.mockResolvedValueOnce(false);
    renderHook(() => useUnsavedChangesGuard(true));
    const opts = useBlockerSpy.mock.calls.at(-1)![0] as {
      disabled: boolean;
      shouldBlockFn: () => Promise<boolean>;
    };
    expect(opts.disabled).toBe(false);
    await expect(opts.shouldBlockFn()).resolves.toBe(true);
  });

  it("lets navigation through when the operator confirms leaving", async () => {
    confirmSpy.mockResolvedValueOnce(true);
    renderHook(() => useUnsavedChangesGuard(true));
    const opts = useBlockerSpy.mock.calls.at(-1)![0] as {
      shouldBlockFn: () => Promise<boolean>;
    };
    await expect(opts.shouldBlockFn()).resolves.toBe(false);
  });
});
