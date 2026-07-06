import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { invalidateTelemetryQueries } from "./telemetry-query-invalidation";

function fakeQueryClient() {
  return { invalidateQueries: vi.fn().mockResolvedValue(undefined) };
}

describe("invalidateTelemetryQueries", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("коалесцирует burst в один сброс через 2s", async () => {
    const qc = fakeQueryClient();
    invalidateTelemetryQueries(qc, "a1");
    invalidateTelemetryQueries(qc, "a2");
    expect(qc.invalidateQueries).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2_000);
    // dashboard + servers + два целевых server(id)
    expect(qc.invalidateQueries).toHaveBeenCalledTimes(4);
  });

  it("maxWait: постоянный поток телеметрии всё равно сбрасывается в пределах 10s", async () => {
    const qc = fakeQueryClient();
    for (let i = 0; i < 12; i++) {
      invalidateTelemetryQueries(qc, `a${i}`);
      await vi.advanceTimersByTimeAsync(1_000);
    }
    expect(qc.invalidateQueries.mock.calls.length).toBeGreaterThan(0);
  });

  it("изолирует состояние между разными QueryClient", async () => {
    const qc1 = fakeQueryClient();
    const qc2 = fakeQueryClient();
    invalidateTelemetryQueries(qc1, "a1");
    invalidateTelemetryQueries(qc2); // pendingAll только у qc2
    await vi.advanceTimersByTimeAsync(2_000);
    // qc1 получил целевой server("a1") и НЕ получил predicate-сброс "all";
    // qc2 — наоборот. Раньше модульный синглтон смешал бы их состояние
    // (второй вызов затирал таймер первого, pendingAll тёк между клиентами).
    const hasPredicate = (qc: ReturnType<typeof fakeQueryClient>) =>
      qc.invalidateQueries.mock.calls.some(
        (c) => typeof (c[0] as { predicate?: unknown }).predicate === "function",
      );
    expect(JSON.stringify(qc1.invalidateQueries.mock.calls)).toContain("a1");
    expect(hasPredicate(qc1)).toBe(false);
    expect(hasPredicate(qc2)).toBe(true);
  });
});
