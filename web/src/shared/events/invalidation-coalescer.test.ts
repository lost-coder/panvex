import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createCoalescer } from "./invalidation-coalescer";

describe("createCoalescer", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("trailing: одиночный вызов срабатывает через trailingMs", () => {
    const c = createCoalescer({ trailingMs: 2_000, maxWaitMs: 10_000 });
    const flush = vi.fn();
    c.schedule(flush);
    vi.advanceTimersByTime(1_999);
    expect(flush).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(flush).toHaveBeenCalledTimes(1);
  });

  it("maxWait: непрерывный поток не откладывает сброс дольше maxWaitMs", () => {
    const c = createCoalescer({ trailingMs: 2_000, maxWaitMs: 10_000 });
    const flush = vi.fn();
    // Вызов каждую секунду — trailing-край постоянно сдвигается.
    for (let i = 0; i < 15; i++) {
      c.schedule(flush);
      vi.advanceTimersByTime(1_000);
    }
    // Без maxWait старая реализация не сработала бы ни разу.
    expect(flush.mock.calls.length).toBeGreaterThanOrEqual(1);
  });

  it("leading: первый вызов в тихом окне срабатывает сразу, шторм коалесцируется", () => {
    const c = createCoalescer({ trailingMs: 2_000, maxWaitMs: 10_000, leading: true });
    const flush = vi.fn();
    c.schedule(flush);
    expect(flush).toHaveBeenCalledTimes(1); // leading edge

    // Шторм в течение окна — только один trailing-сброс.
    c.schedule(flush);
    c.schedule(flush);
    c.schedule(flush);
    expect(flush).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(2_000);
    expect(flush).toHaveBeenCalledTimes(2);
  });

  it("cancel: отменяет отложенный сброс", () => {
    const c = createCoalescer({ trailingMs: 2_000, maxWaitMs: 10_000 });
    const flush = vi.fn();
    c.schedule(flush);
    c.cancel();
    vi.advanceTimersByTime(20_000);
    expect(flush).not.toHaveBeenCalled();
  });
});
