import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { apiClient } from "@/shared/api/api";
import type { EnrollmentAttemptDetail } from "@/shared/api/types-enrollment";
import { enrollmentAttemptsKeys } from "@/features/enrollment-attempts/queryKeys";

const LIST_POLL_MS = 5_000;
const DETAIL_POLL_MS = 3_000;

interface LiveAttempt {
  /** Most recent attempt id for the agent, or null if none is recorded yet. */
  latestId: string | null;
  /** Full detail (events) for the latest attempt — null while loading. */
  detail: EnrollmentAttemptDetail | null;
  /** True until the first list/detail roundtrip resolves. */
  isLoading: boolean;
}

/**
 * useEnrollmentLiveAttempt polls /api/enrollment-attempts for the
 * most recent attempt belonging to the given agent and then keeps the
 * timeline (events) fresh by polling the detail endpoint.
 *
 * The backend already publishes `enrollment.event` /
 * `enrollment.completed` / `enrollment.failed` on the /events
 * WebSocket, but the global EventsSynchronizer does not yet route those
 * to React Query keys (see shared/events/event-invalidations.ts) — and
 * the in-flight enrollment finishes in a handful of seconds, so a
 * 3-second poll on detail keeps the UI responsive without the extra
 * wiring. The detail query stops polling once the attempt reaches a
 * terminal state.
 */
export function useEnrollmentLiveAttempt(agentId: string | null): LiveAttempt {
  const list = useQuery({
    queryKey: enrollmentAttemptsKeys.byAgent(agentId!),
    queryFn: ({ signal }) =>
      apiClient.listEnrollmentAttempts({ agent_id: agentId!, limit: 1 }, { signal }),
    enabled: !!agentId,
    refetchInterval: LIST_POLL_MS,
  });

  // Derive the latest attempt id directly from the list query rather
  // than mirroring it into local state — that keeps the React Query
  // cache as the single source of truth and avoids the cascading
  // setState-in-effect warning from react-hooks 7.x.
  const latestId = list.data?.items[0]?.id ?? null;

  const detail = useQuery({
    queryKey: enrollmentAttemptsKeys.detail(latestId!),
    queryFn: () => apiClient.getEnrollmentAttempt(latestId!),
    enabled: !!latestId,
    // Stop polling once the attempt reaches a terminal state — the
    // events log is append-only and the backend will not mutate it.
    refetchInterval: (q) => {
      const status = q.state.data?.attempt.status;
      if (status === "success" || status === "failed") return false;
      return DETAIL_POLL_MS;
    },
  });

  return useMemo(
    () => ({
      latestId,
      detail: detail.data ?? null,
      isLoading: list.isLoading || (latestId !== null && detail.isLoading),
    }),
    [latestId, detail.data, detail.isLoading, list.isLoading],
  );
}
