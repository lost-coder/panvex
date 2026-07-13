import { describe, expect, it, vi } from "vitest";
import { submitAndClose } from "./submit-and-close";

describe("submitAndClose", () => {
  it("закрывает после успешного action", async () => {
    const close = vi.fn();
    await submitAndClose(() => Promise.resolve("ok"), close);
    expect(close).toHaveBeenCalledTimes(1);
  });

  it("НЕ закрывает после reject и не пробрасывает ошибку", async () => {
    const close = vi.fn();
    await expect(
      submitAndClose(() => Promise.reject(new Error("boom")), close),
    ).resolves.toBeUndefined();
    expect(close).not.toHaveBeenCalled();
  });

  it("ретрай после ошибки закрывает (сценарий stale-error, §1.2)", async () => {
    const close = vi.fn();
    await submitAndClose(() => Promise.reject(new Error("first")), close);
    await submitAndClose(() => Promise.resolve("second"), close);
    expect(close).toHaveBeenCalledTimes(1);
  });
});
