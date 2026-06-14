// 2.4: declarative map from WebSocket event type to React Query
// invalidations. Kept out of the EventsSynchronizer provider so it
// can be unit-tested and later reused from the features/* slices
// after the migration (shared/events/ lives here pre-Phase-4).
//
// BP-02: keys are read from the owning feature's factory rather
// than inline string literals so cache identity stays canonical
// across the codebase. Shapes are unchanged — the factories
// preserve the original tuples verbatim.

import { auditKeys, jobsKeys } from "@/features/activity/queryKeys";
import { clientsKeys } from "@/features/clients/queryKeys";
import { agentsKeys, controlRoomKeys } from "@/features/servers/queryKeys";

export interface EventEnvelope {
  type: string;
  data: unknown;
}

export interface EventInvalidation {
  /** queryKey prefixes to invalidate immediately. */
  keys: readonly (readonly unknown[])[];
  /** When true, trigger the debounced telemetry invalidation. */
  telemetry?: boolean | undefined;
  /** Optional agent id so a targeted telemetry invalidate is possible. */
  telemetryAgentID?: string | undefined;
}

export function extractAgentID(data: unknown): string | undefined {
  if (typeof data !== "object" || data === null) return undefined;
  const record = data as Record<string, unknown>;
  const candidate = record.agent_id ?? record.id;
  return typeof candidate === "string" ? candidate : undefined;
}

/**
 * invalidationsForEvent resolves a WS event envelope to the set of
 * React Query keys that must be refreshed and optional telemetry
 * flags. Unknown types fall back to a broad sweep so a new backend
 * event still refreshes live data while this map catches up; the
 * caller is expected to log a debug line in that case.
 */
export function invalidationsForEvent(event: EventEnvelope): EventInvalidation {
  const agentID = extractAgentID(event.data);
  if (event.type.startsWith("agents.")) {
    return {
      keys: [controlRoomKeys.all, agentsKeys.all],
      telemetry: true,
      telemetryAgentID: agentID,
    };
  }
  if (event.type.startsWith("jobs.")) {
    return { keys: [jobsKeys.all, controlRoomKeys.all] };
  }
  if (event.type === "audit.created") {
    return { keys: [auditKeys.all] };
  }
  if (event.type.startsWith("clients.")) {
    return { keys: [clientsKeys.all, controlRoomKeys.all] };
  }
  if (event.type === "runtime.events") {
    // Runtime log records do not change any query data; the server-detail
    // page consumes them via its own WebSocket (useAgentRuntimeEvents).
    // Returning no keys keeps a chatty agent from triggering the broad
    // fallback sweep on every log batch (D6a).
    return { keys: [] };
  }
  return {
    keys: [
      controlRoomKeys.all,
      agentsKeys.all,
      clientsKeys.all,
      auditKeys.all,
      jobsKeys.all,
    ],
    telemetry: true,
  };
}

export function isKnownEventType(type: string): boolean {
  return (
    type.startsWith("agents.") ||
    type.startsWith("jobs.") ||
    type === "audit.created" ||
    type.startsWith("clients.") ||
    type === "runtime.events"
  );
}
