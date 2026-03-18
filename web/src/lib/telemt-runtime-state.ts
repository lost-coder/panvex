import type { Agent } from "./api";

export type AgentRuntimeStatusTone = "emerald" | "amber" | "rose" | "sky";

export type AgentRuntimeStatus = {
  label: "Healthy" | "Degraded" | "Offline" | "Starting";
  tone: AgentRuntimeStatusTone;
};

export function buildAgentRuntimeStatus(agent: Agent): AgentRuntimeStatus {
  if (agent.presence_state === "offline") {
    return { label: "Offline", tone: "rose" };
  }

  if (agent.runtime.startup_status === "pending" || agent.runtime.startup_status === "initializing" || agent.runtime.initialization_status === "pending" || agent.runtime.initialization_status === "initializing") {
    return { label: "Starting", tone: "sky" };
  }

  if (
    agent.runtime.degraded ||
    !agent.runtime.accepting_new_connections ||
    (agent.runtime.dc_coverage_pct > 0 && agent.runtime.dc_coverage_pct < 100) ||
    (agent.runtime.total_upstreams > 0 && agent.runtime.healthy_upstreams < agent.runtime.total_upstreams)
  ) {
    return { label: "Degraded", tone: "amber" };
  }

  return { label: "Healthy", tone: "emerald" };
}

export function buildAgentModeLabel(agent: Agent): "Middle" | "Direct" | "Fallback" {
  if (agent.runtime.use_middle_proxy && agent.runtime.me2dc_fallback_enabled && !agent.runtime.me_runtime_ready) {
    return "Fallback";
  }

  if (agent.runtime.use_middle_proxy) {
    return "Middle";
  }

  return "Direct";
}

export function buildAgentConnectionSummary(agent: Agent): { primary: string; secondary: string } {
  return {
    primary: String(agent.runtime.current_connections),
    secondary: `ME ${agent.runtime.current_connections_me} / Direct ${agent.runtime.current_connections_direct}`
  };
}

export function buildAgentAttentionList(agents: Agent[]): Agent[] {
  return [...agents]
    .filter((agent) => {
      const status = buildAgentRuntimeStatus(agent);
      return status.label === "Offline" || status.label === "Degraded";
    })
    .sort((left, right) => attentionScore(right) - attentionScore(left));
}

function attentionScore(agent: Agent): number {
  const status = buildAgentRuntimeStatus(agent);
  switch (status.label) {
    case "Offline":
      return 3;
    case "Degraded":
      return 2;
    case "Starting":
      return 1;
    default:
      return 0;
  }
}
