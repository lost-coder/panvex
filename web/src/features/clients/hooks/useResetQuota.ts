// Reset-quota Phase 2: hook owns the "fire job + poll until terminal +
// surface per-target outcome" state machine. Splitting it out of the
// container keeps the page composition uncluttered and lets the cell
// component subscribe to per-agent state without the parent threading
// every flag through props.
//
// Lifecycle:
//   1. caller fires `reset(agentId?)` — when agentId is undefined we
//      use the fan-out variant; otherwise we hit the per-agent endpoint.
//   2. backend returns the freshly-created job; we remember its id and
//      poll /api/jobs every 2 s while at least one watched job is still
//      non-terminal. When all watched jobs are terminal the polling
//      naturally winds down (`refetchInterval` returns `false`).
//   3. each call to `reset(agentId)` populates `rowStates[agentId]`
//      synchronously to "pending" so the row spinner shows up before
//      the network call returns. The fan-out variant pre-seeds every
//      known deployment agent.
//   4. once a target reaches a terminal status we parse `result_json`,
//      derive a `success | unsupported | readonly | failed` outcome,
//      and stash it on `rowStates[agentId]`. The container can then
//      decide whether to toast (success) or render inline (failure).
//
// The hook deliberately does NOT own the toast — that's the container's
// job, because the success-toast message differs between the per-agent
// and fan-out flows and the hook should stay agnostic of strings.

import { useCallback, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient, type Job } from "@/shared/api/api";
import { clientsKeys } from "@/features/clients/queryKeys";

/**
 * Outcome of a single (job, target) pair once it reaches a terminal
 * status. The hook exposes this so the cell can pick the right
 * translated inline message instead of having to parse `result_json`
 * itself.
 */
export type ResetOutcome =
  | { kind: "pending" }
  | { kind: "success" }
  | { kind: "unsupported" }
  | { kind: "readonly" }
  | { kind: "failed"; error: string };

type WatchedJob = {
  /** Job.id returned by the backend on the POST. */
  jobId: string;
  /** Agents whose targets we still need to read out. */
  pendingAgentIds: Set<string>;
  /** Marker used by the success-toast callback. Per-agent flow surfaces
   *  a "Quota reset on {{agent}}" message; fan-out swaps it for
   *  "Quota reset on {{count}} server(s)". */
  scope: "agent" | "all";
};

const TERMINAL_STATUSES: ReadonlySet<string> = new Set([
  "succeeded",
  "failed",
  "expired",
]);

/**
 * Narrowing of the wire-level `result_json` blob carried on the
 * terminal target. Every field is optional — we parse defensively so a
 * future backend addition doesn't blow up the UI.
 */
interface ResetResultJson {
  used_bytes?: number;
  last_reset_epoch_secs?: number;
  unsupported_telemt?: boolean;
  read_only_telemt?: boolean;
}

function parseResultJson(raw: string): ResetResultJson {
  if (!raw) return {};
  try {
    const obj = JSON.parse(raw) as unknown;
    if (!obj || typeof obj !== "object") return {};
    return obj as ResetResultJson;
  } catch {
    // The backend wire shape is JSON-text; a malformed payload here is
    // a bug to surface upstream, but the cell still wants a graceful
    // "generic failed" rendering instead of a hard crash.
    return {};
  }
}

function outcomeFromTarget(target: Job["targets"][number]): ResetOutcome {
  const result = parseResultJson(target.result_json);
  if (target.status === "succeeded") {
    return { kind: "success" };
  }
  // Flag-first: when the agent typed unsupported/read-only we already
  // know the right translated message; only fall back to result_text
  // for the generic failed branch.
  if (result.unsupported_telemt === true) {
    return { kind: "unsupported" };
  }
  if (result.read_only_telemt === true) {
    return { kind: "readonly" };
  }
  return { kind: "failed", error: target.result_text || target.status };
}

export interface UseResetQuotaResult {
  /** Per-agent outcome map. Absence = "no reset in flight or recorded". */
  rowStates: Record<string, ResetOutcome>;
  /** Whether the fan-out flow is currently active. Detail page uses
   *  this to disable the "Reset everywhere" button while a job is in
   *  flight. */
  fanOutPending: boolean;
  /**
   * Fire the per-agent variant. Pre-seeds `rowStates[agentId]` to
   * "pending" synchronously so the cell can flip to a spinner before
   * the POST resolves. Resolves when the job's target reaches a
   * terminal state (success or failure); the hook also exposes the
   * outcome on `rowStates` for subscriber components.
   */
  resetOnAgent: (agentId: string) => Promise<ResetOutcome>;
  /**
   * Fire the fan-out variant. Resolves with a map of per-agent
   * outcomes once every target is terminal.
   */
  resetEverywhere: (agentIds: string[]) => Promise<Record<string, ResetOutcome>>;
  /**
   * Dismiss the inline state for one row. Used when the operator
   * clicks the "ok" affordance on a failure message.
   */
  clearRow: (agentId: string) => void;
}

/**
 * @param clientId - the client whose detail-query should be invalidated
 *   on every per-target success so the page picks up the reset
 *   `quotaUsedBytes=0` immediately.
 * @param onSuccessToast - container hook for surfacing the success
 *   toast. Keeps i18n strings out of the hook itself.
 */
export function useResetQuota(
  clientId: string,
  onSuccessToast: (scope: "agent" | "all", payload: { agentId?: string; count?: number }) => void,
): UseResetQuotaResult {
  const qc = useQueryClient();
  const [rowStates, setRowStates] = useState<Record<string, ResetOutcome>>({});
  const [watched, setWatched] = useState<WatchedJob[]>([]);
  // Resolvers waiting on terminal status, keyed by (jobId, agentId).
  // Stored on a ref so the polling callback can settle them without
  // re-rendering the hook every time a deferred caller registers.
  const resolversRef = useRef<
    Map<string, (outcome: ResetOutcome) => void>
  >(new Map());
  // Fan-out callers wait on the whole job, not individual targets;
  // store one resolver per active fan-out job so we can hand back the
  // aggregate map once every target is terminal.
  const fanOutResolversRef = useRef<
    Map<string, (outcomes: Record<string, ResetOutcome>) => void>
  >(new Map());
  const fanOutOutcomesRef = useRef<Map<string, Record<string, ResetOutcome>>>(
    new Map(),
  );

  const hasWatchers = watched.length > 0;

  // Poll /api/jobs while at least one watched job is still in flight.
  // We keep the cadence intentionally generous (2 s) — the job pipeline
  // is not real-time and a tighter loop would hammer the panel without
  // improving the UX (Telemt's reset round-trip is dominated by the
  // gRPC + Telemt-side latency, not panel polling).
  useQuery({
    queryKey: ["clients", clientId, "reset-quota-jobs"],
    queryFn: async () => {
      const jobs = await apiClient.jobs();
      processJobs(jobs);
      return jobs;
    },
    enabled: hasWatchers,
    refetchInterval: hasWatchers ? 2000 : false,
    // Don't keep the result around — we only care about side-effects.
    staleTime: 0,
    gcTime: 0,
  });

  const processJobs = useCallback(
    (jobs: Job[]) => {
      if (watched.length === 0) return;
      const byId = new Map<string, Job>();
      for (const j of jobs) byId.set(j.id, j);

      // Walk every watched job and settle whichever targets reached
      // terminal status since the last tick.
      const nextWatched: WatchedJob[] = [];
      let invalidatedDetail = false;
      const successesByJob = new Map<string, string[]>();
      for (const w of watched) {
        const job = byId.get(w.jobId);
        if (!job) {
          // Job hasn't landed in the list yet (race against the first
          // poll after POST). Keep watching.
          nextWatched.push(w);
          continue;
        }
        const stillPending = new Set<string>();
        for (const agentId of w.pendingAgentIds) {
          const target = job.targets.find((t) => t.agent_id === agentId);
          if (!target || !TERMINAL_STATUSES.has(target.status)) {
            stillPending.add(agentId);
            continue;
          }
          const outcome = outcomeFromTarget(target);
          setRowStates((prev) => ({ ...prev, [agentId]: outcome }));
          const resolver = resolversRef.current.get(`${w.jobId}:${agentId}`);
          if (resolver) {
            resolver(outcome);
            resolversRef.current.delete(`${w.jobId}:${agentId}`);
          }
          const fanOutMap = fanOutOutcomesRef.current.get(w.jobId);
          if (fanOutMap) fanOutMap[agentId] = outcome;
          if (outcome.kind === "success") {
            invalidatedDetail = true;
            const arr = successesByJob.get(w.jobId) ?? [];
            arr.push(agentId);
            successesByJob.set(w.jobId, arr);
          }
        }
        if (stillPending.size > 0) {
          nextWatched.push({ ...w, pendingAgentIds: stillPending });
          continue;
        }
        // Fan-out callers wait on the aggregate map; settle them now.
        const fanResolver = fanOutResolversRef.current.get(w.jobId);
        if (fanResolver) {
          fanResolver(fanOutOutcomesRef.current.get(w.jobId) ?? {});
          fanOutResolversRef.current.delete(w.jobId);
          fanOutOutcomesRef.current.delete(w.jobId);
        }
      }

      // Fire toasts and detail invalidation once per tick to keep the
      // notification surface non-spammy.
      if (invalidatedDetail) {
        void qc.invalidateQueries({ queryKey: clientsKeys.detail(clientId) });
        void qc.invalidateQueries({ queryKey: clientsKeys.all });
      }
      for (const [jobId, agentIds] of successesByJob.entries()) {
        const w = watched.find((x) => x.jobId === jobId);
        if (!w) continue;
        if (w.scope === "agent") {
          for (const a of agentIds) {
            onSuccessToast("agent", { agentId: a });
          }
        }
        // Fan-out scope: defer the aggregate toast until every target
        // settles so we surface a single "Reset on N servers" message.
      }
      // Surface the fan-out aggregate toast when all of a fan-out
      // job's targets are now resolved.
      for (const w of watched) {
        if (w.scope !== "all") continue;
        const stillIn = nextWatched.find((x) => x.jobId === w.jobId);
        if (stillIn) continue;
        const total = fanOutOutcomesRef.current.get(w.jobId);
        // The map has already been drained above; recover the count
        // from `watched` (its pendingAgentIds size before drain) by
        // counting successes recorded in rowStates this tick.
        const succeeded = (successesByJob.get(w.jobId) ?? []).length;
        if (succeeded > 0 && !total) {
          onSuccessToast("all", { count: succeeded });
        }
      }

      if (nextWatched.length !== watched.length) {
        setWatched(nextWatched);
      }
    },
    [watched, qc, clientId, onSuccessToast],
  );

  const fanOutPending = useMemo(
    () => watched.some((w) => w.scope === "all"),
    [watched],
  );

  const onAgentMutation = useMutation({
    mutationFn: ({ agentId }: { agentId: string }) =>
      apiClient.resetClientQuotaOnAgent(clientId, agentId),
    onSuccess: (response, vars) => {
      setWatched((prev) => [
        ...prev,
        {
          jobId: response.job.id,
          pendingAgentIds: new Set([vars.agentId]),
          scope: "agent",
        },
      ]);
    },
    onError: (err: Error, vars) => {
      // Surface as a generic failure — the POST itself blew up before
      // any job was created (network, 403, validation). The cell will
      // render the error inline.
      setRowStates((prev) => ({
        ...prev,
        [vars.agentId]: { kind: "failed", error: err.message },
      }));
    },
  });

  const fanOutMutation = useMutation({
    mutationFn: ({ agentIds: _agentIds }: { agentIds: string[] }) =>
      apiClient.resetClientQuotaFanOut(clientId),
    onSuccess: (response, vars) => {
      const pendingSet = new Set(vars.agentIds);
      // If the backend returned a target list, prefer it as the
      // authoritative set — that's the source of truth for what
      // actually got queued.
      if (response.job.targets.length > 0) {
        pendingSet.clear();
        for (const t of response.job.targets) pendingSet.add(t.agent_id);
      }
      fanOutOutcomesRef.current.set(response.job.id, {});
      setWatched((prev) => [
        ...prev,
        {
          jobId: response.job.id,
          pendingAgentIds: pendingSet,
          scope: "all",
        },
      ]);
    },
    onError: (err: Error, vars) => {
      // POST itself failed — fan failure across the rows we pre-
      // seeded so the operator sees the issue everywhere.
      setRowStates((prev) => {
        const next = { ...prev };
        for (const a of vars.agentIds) {
          next[a] = { kind: "failed", error: err.message };
        }
        return next;
      });
    },
  });

  const resetOnAgent = useCallback(
    async (agentId: string): Promise<ResetOutcome> => {
      setRowStates((prev) => ({ ...prev, [agentId]: { kind: "pending" } }));
      const response = await onAgentMutation.mutateAsync({ agentId });
      return new Promise<ResetOutcome>((resolve) => {
        resolversRef.current.set(`${response.job.id}:${agentId}`, resolve);
      });
    },
    [onAgentMutation],
  );

  const resetEverywhere = useCallback(
    async (agentIds: string[]): Promise<Record<string, ResetOutcome>> => {
      setRowStates((prev) => {
        const next = { ...prev };
        for (const a of agentIds) next[a] = { kind: "pending" };
        return next;
      });
      const response = await fanOutMutation.mutateAsync({ agentIds });
      return new Promise<Record<string, ResetOutcome>>((resolve) => {
        fanOutResolversRef.current.set(response.job.id, resolve);
      });
    },
    [fanOutMutation],
  );

  const clearRow = useCallback((agentId: string) => {
    setRowStates((prev) => {
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { [agentId]: _drop, ...rest } = prev;
      return rest;
    });
  }, []);

  return { rowStates, fanOutPending, resetOnAgent, resetEverywhere, clearRow };
}
