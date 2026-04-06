export async function invalidateTelemetryQueries(
  queryClient: {
    invalidateQueries: (input: Record<string, unknown>) => Promise<unknown> | unknown;
  },
  agentID?: string
) {
  await queryClient.invalidateQueries({ queryKey: ["telemetry", "dashboard"] });
  await queryClient.invalidateQueries({ queryKey: ["telemetry", "servers"] });
  if (agentID) {
    await queryClient.invalidateQueries({ queryKey: ["telemetry", "server", agentID] });
    return;
  }
  await queryClient.invalidateQueries({
    predicate: (query: { queryKey: unknown[] }) =>
      query.queryKey[0] === "telemetry" && query.queryKey[1] === "server",
  });
}
