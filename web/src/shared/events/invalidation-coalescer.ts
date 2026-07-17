// 7.4 (#web-7/8): trailing-debounce with an upper wait bound (maxWait)
// and an optional leading edge. A shared primitive for WS invalidations:
//   - telemetry: trailing 2s / maxWait 10s (smoothing is preserved,
//     but a sustained stream no longer defers the flush forever);
//   - non-telemetry keys: leading + trailing 2s / maxWait 10s
//     (a single event is reflected immediately, a storm collapses to one refetch).
//
// No external dependencies — lodash.debounce with maxWait would pull in a package
// for 40 lines' worth of logic.

export interface CoalescerOptions {
  /** Quiet window: flush trailingMs after the last schedule. */
  trailingMs: number;
  /** Upper bound: flush no later than maxWaitMs after the first schedule. */
  maxWaitMs: number;
  /** The first schedule in a quiet window flushes immediately. */
  leading?: boolean;
}

export interface Coalescer {
  schedule(flush: () => void): void;
  cancel(): void;
}

export function createCoalescer(opts: CoalescerOptions): Coalescer {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let firstPendingAt: number | null = null;
  let pendingFlush: (() => void) | null = null;
  // Start of the "quiet window" for the leading edge: right after a flush, new
  // schedules within trailingMs go to the trailing edge, not the leading one.
  let lastFlushAt = Number.NEGATIVE_INFINITY;

  const fireTrailing = () => {
    timer = null;
    firstPendingAt = null;
    const flush = pendingFlush;
    pendingFlush = null;
    if (flush) {
      lastFlushAt = Date.now();
      flush();
    }
  };

  return {
    schedule(flush) {
      const now = Date.now();
      if (
        opts.leading === true &&
        pendingFlush === null &&
        timer === null &&
        now - lastFlushAt >= opts.trailingMs
      ) {
        lastFlushAt = now;
        flush();
        return;
      }
      pendingFlush = flush;
      firstPendingAt ??= now;
      if (timer !== null) clearTimeout(timer);
      const remainingMax = firstPendingAt + opts.maxWaitMs - now;
      timer = setTimeout(fireTrailing, Math.max(0, Math.min(opts.trailingMs, remainingMax)));
    },
    cancel() {
      if (timer !== null) clearTimeout(timer);
      timer = null;
      firstPendingAt = null;
      pendingFlush = null;
    },
  };
}
