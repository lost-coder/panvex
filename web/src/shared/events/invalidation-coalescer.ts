// 7.4 (#web-7/8): trailing-debounce с верхней границей ожидания (maxWait)
// и опциональным leading-краем. Общий примитив для WS-инвалидаций:
//   - телеметрия: trailing 2s / maxWait 10s (сглаживание сохраняется,
//     но постоянный поток больше не откладывает сброс бесконечно);
//   - не-телеметрийные ключи: leading + trailing 2s / maxWait 10s
//     (одиночное событие отражается немедленно, шторм — один рефетч).
//
// Без внешних зависимостей — lodash.debounce с maxWait тянул бы пакет
// ради 40 строк.

export interface CoalescerOptions {
  /** Тихое окно: сброс через trailingMs после последнего schedule. */
  trailingMs: number;
  /** Верхняя граница: сброс не позже maxWaitMs после первого schedule. */
  maxWaitMs: number;
  /** Первый schedule в тихом окне сбрасывается немедленно. */
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
  // Начало «тихого окна» для leading-края: сразу после сброса новые
  // schedule в течение trailingMs идут на trailing-край, а не leading.
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
