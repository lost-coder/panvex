type AgentLike = {
  id: string;
};

type InstanceLike = {
  agent_id: string;
};

export type ConnectionTokenStatus = "active" | "expired" | "consumed" | "revoked";

export type ConnectionJourneyStep = {
  key: "server" | "bootstrap" | "runtime";
  label: string;
  detail: string;
  state: "idle" | "active" | "done";
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

export function buildConnectionJourney(tokenStatus: ConnectionTokenStatus, connected: boolean): ConnectionJourneyStep[] {
  if (connected) {
    return [
      buildConnectionJourneyStep("server", "done"),
      buildConnectionJourneyStep("bootstrap", "done"),
      buildConnectionJourneyStep("runtime", "done")
    ];
  }

  if (tokenStatus === "consumed") {
    return [
      buildConnectionJourneyStep("server", "done"),
      buildConnectionJourneyStep("bootstrap", "done"),
      buildConnectionJourneyStep("runtime", "active")
    ];
  }

  return [
    buildConnectionJourneyStep("server", "active"),
    buildConnectionJourneyStep("bootstrap", "idle"),
    buildConnectionJourneyStep("runtime", "idle")
  ];
}

function buildConnectionJourneyStep(key: ConnectionJourneyStep["key"], state: ConnectionJourneyStep["state"]): ConnectionJourneyStep {
  switch (key) {
  case "server":
    return {
      key,
      label: "Server",
      detail: "The installer starts on the Telemt host.",
      state
    };
  case "bootstrap":
    return {
      key,
      label: "Secure bootstrap",
      detail: "Panvex validates the token and issues the agent identity.",
      state
    };
  default:
    return {
      key,
      label: "Runtime signal",
      detail: "The first snapshot confirms that the server is ready.",
      state
    };
  }
}
