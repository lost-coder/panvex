// Trailing-edge debounce that coalesces rapid WebSocket-driven invalidations.
// Each call resets the timer; the actual invalidation fires once after 2s of quiet.
let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let pendingAgentIDs: Set<string> = new Set();
let pendingAll = false;

export function invalidateTelemetryQueries(
  queryClient: {
    invalidateQueries: (input: Record<string, unknown>) => Promise<unknown> | unknown;
  },
  agentID?: string
) {
  if (agentID) {
    pendingAgentIDs.add(agentID);
  } else {
    pendingAll = true;
  }

  if (debounceTimer) clearTimeout(debounceTimer);
  debounceTimer = setTimeout(async () => {
    debounceTimer = null;
    const agentIDs = [...pendingAgentIDs];
    const invalidateAll = pendingAll;
    pendingAgentIDs = new Set();
    pendingAll = false;

    await queryClient.invalidateQueries({ queryKey: ["telemetry", "dashboard"] });
    await queryClient.invalidateQueries({ queryKey: ["telemetry", "servers"] });
    if (invalidateAll) {
      await queryClient.invalidateQueries({
        predicate: (query: { queryKey: unknown[] }) =>
          query.queryKey[0] === "telemetry" && query.queryKey[1] === "server",
      });
    } else {
      for (const id of agentIDs) {
        await queryClient.invalidateQueries({ queryKey: ["telemetry", "server", id] });
      }
    }
  }, 2000);
}
