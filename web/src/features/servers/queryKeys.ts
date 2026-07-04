// BP-02: feature-local React-Query key factory.
//
// "Servers" is the operator-visible UI label; the backend / cache
// keys use the canonical "agents" + "telemetry" namespaces (see
// src/CLAUDE.md "node vs agent vs server"). Two concerns share this
// feature folder so they share the factory:
//   * agents — flat enrollment list (`apiClient.agents()`)
//   * telemetry — server cards, dashboards, history
//
// Shapes preserved verbatim from the pre-migration code.

export const agentsKeys = {
  /** Root prefix — flat list of enrolled agents. */
  all: ["agents"] as const,
  /** Unfiltered list — same shape as `all` (preserved verbatim). */
  list: () => [...agentsKeys.all] as const,
};

export const telemetryKeys = {
  /** Root prefix — invalidate to flush every telemetry query. */
  all: ["telemetry"] as const,

  /** Dashboard-overview snapshot. */
  dashboard: () => [...telemetryKeys.all, "dashboard"] as const,

  /** All servers grid. */
  servers: () => [...telemetryKeys.all, "servers"] as const,

  /** Per-server detail. */
  server: (id: string) => [...telemetryKeys.all, "server", id] as const,

  /** Per-server load history (resolution-aware). */
  serverLoadHistory: (id: string, from?: string, to?: string) =>
    [...telemetryKeys.server(id), "history", "load", from, to] as const,

  /** Per-server DC-health history. */
  serverDCHistory: (id: string, from?: string, to?: string) =>
    [...telemetryKeys.server(id), "history", "dc", from, to] as const,
};

export const fleetGroupsKeys = {
  /** Root prefix — invalidate when membership changes anywhere. */
  all: ["fleet-groups"] as const,
  /** Unfiltered list — same shape as `all` (preserved verbatim). */
  list: () => [...fleetGroupsKeys.all] as const,
};

export const controlRoomKeys = {
  /** Root prefix — dashboard control-room aggregate. */
  all: ["control-room"] as const,
};

export const configKeys = {
  /** Per-agent override/effective/observed config + drift. */
  agent: (agentId: string) => ["config", "agent", agentId] as const,

  /** Poll the single-apply persistent batch-of-one (P3-3.4). */
  agentApplyBatch: (agentId: string, batchId: string) =>
    ["config", "agent", agentId, "apply-batch", batchId] as const,

  /** Per-fleet-group baseline config + member node statuses. */
  group: (groupId: string) => ["config", "group", groupId] as const,

  /** Persistent-batch aggregate (resumable rollout view), keyed by batch id. */
  groupApplyBatch: (groupId: string, batchId: string) =>
    ["config", "group", groupId, "apply-batch", batchId] as const,

  /** The fleet group's currently-running batch, if any (resume-on-mount). */
  activeGroupApplyBatch: (groupId: string) =>
    ["config", "group", groupId, "apply-batch", "active"] as const,
};
