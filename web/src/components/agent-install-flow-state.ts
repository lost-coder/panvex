type AgentLike = {
  id: string;
};

type InstanceLike = {
  agent_id: string;
};

export function resolveConnectedAgentID(agents: AgentLike[], instances: InstanceLike[], baselineAgentIDs: string[]) {
  const baseline = new Set(baselineAgentIDs);
  const candidate = agents.find((agent) => !baseline.has(agent.id));
  if (!candidate) {
    return null;
  }

  const hasRuntimeInstance = instances.some((instance) => instance.agent_id === candidate.id);
  if (!hasRuntimeInstance) {
    return null;
  }

  return candidate.id;
}
