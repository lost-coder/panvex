// Коалесцирует rapid WebSocket-driven инвалидации телеметрии: trailing 2s
// + maxWait 10s (7.4, #web-7 — раньше плотный поток телеметрии бесконечно
// сдвигал trailing-край и инвалидация не срабатывала вовсе).
//
// BP-02: telemetry keys come from the servers feature factory so
// EventsSynchronizer and useTelemetry stay aligned on cache identity.
//
// 7.4 (#web-8): состояние — per-QueryClient (WeakMap), а не модульный
// синглтон: пересозданный клиент (логаут/тесты) не наследует чужие
// pending-агенты и таймер.

import { telemetryKeys } from "@/features/servers/queryKeys";
import { type Coalescer, createCoalescer } from "./invalidation-coalescer";

export const TELEMETRY_TRAILING_MS = 2_000;
export const TELEMETRY_MAX_WAIT_MS = 10_000;

type QueryClientLike = {
  invalidateQueries: (input: Record<string, unknown>) => unknown;
};

interface PendingTelemetry {
  coalescer: Coalescer;
  agentIDs: Set<string>;
  all: boolean;
}

const stateByClient = new WeakMap<QueryClientLike, PendingTelemetry>();

export function invalidateTelemetryQueries(
  queryClient: QueryClientLike,
  agentID?: string,
) {
  let state = stateByClient.get(queryClient);
  if (!state) {
    state = {
      coalescer: createCoalescer({
        trailingMs: TELEMETRY_TRAILING_MS,
        maxWaitMs: TELEMETRY_MAX_WAIT_MS,
      }),
      agentIDs: new Set(),
      all: false,
    };
    stateByClient.set(queryClient, state);
  }

  if (agentID) {
    state.agentIDs.add(agentID);
  } else {
    state.all = true;
  }

  const pending = state;
  pending.coalescer.schedule(() => {
    const agentIDs = [...pending.agentIDs];
    const invalidateAll = pending.all;
    pending.agentIDs = new Set();
    pending.all = false;
    void flushTelemetry(queryClient, agentIDs, invalidateAll);
  });
}

async function flushTelemetry(
  queryClient: QueryClientLike,
  agentIDs: string[],
  invalidateAll: boolean,
): Promise<void> {
  await queryClient.invalidateQueries({ queryKey: telemetryKeys.dashboard() });
  await queryClient.invalidateQueries({ queryKey: telemetryKeys.servers() });
  if (invalidateAll) {
    await queryClient.invalidateQueries({
      predicate: (query: { queryKey: unknown[] }) =>
        query.queryKey[0] === "telemetry" && query.queryKey[1] === "server",
    });
  } else {
    for (const id of agentIDs) {
      await queryClient.invalidateQueries({ queryKey: telemetryKeys.server(id) });
    }
  }
}
