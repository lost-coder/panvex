// 2.4: declarative map from WebSocket event type to React Query
// invalidations. Kept out of the EventsSynchronizer provider so it
// can be unit-tested and later reused from the features/* slices
// after the migration (shared/events/ lives here pre-Phase-4).

export interface EventEnvelope {
  type: string;
  data: unknown;
}

export interface EventInvalidation {
  /** queryKey prefixes to invalidate immediately. */
  keys: readonly (readonly unknown[])[];
  /** When true, trigger the debounced telemetry invalidation. */
  telemetry?: boolean;
  /** Optional agent id so a targeted telemetry invalidate is possible. */
  telemetryAgentID?: string;
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
      keys: [["control-room"], ["agents"]],
      telemetry: true,
      telemetryAgentID: agentID,
    };
  }
  if (event.type.startsWith("jobs.")) {
    return { keys: [["jobs"], ["control-room"]] };
  }
  if (event.type === "audit.created") {
    return { keys: [["audit"]] };
  }
  if (event.type.startsWith("clients.")) {
    return { keys: [["clients"], ["control-room"]] };
  }
  return {
    keys: [["control-room"], ["agents"], ["clients"], ["audit"], ["jobs"]],
    telemetry: true,
  };
}

export function isKnownEventType(type: string): boolean {
  return (
    type.startsWith("agents.") ||
    type.startsWith("jobs.") ||
    type === "audit.created" ||
    type.startsWith("clients.")
  );
}
