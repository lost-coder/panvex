import type { Agent } from "./api";

export type AgentRuntimeStatusTone = "emerald" | "amber" | "rose" | "sky";

export type AgentRuntimeStatus = {
  label: "Healthy" | "Degraded" | "Offline" | "Starting";
  tone: AgentRuntimeStatusTone;
};

export type AgentAttentionReason = {
  label: string;
  tone: Exclude<AgentRuntimeStatusTone, "sky">;
};

export type AgentDCSegment = {
  dc: number;
  label: string;
  stage: 0 | 33 | 66 | 100;
  stateLabel: "Full" | "Reduced" | "Limited" | "Down";
  coveragePct: number;
};

export type AgentDCIssueSummary = {
  totalCount: number;
  okCount: number;
  issueCount: number;
  issues: Array<{
    dc: number;
    label: string;
    stateLabel: "Reduced" | "Limited" | "Down";
    tone: "sky" | "amber" | "rose";
  }>;
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

export function buildAgentAttentionReasons(agent: Agent): AgentAttentionReason[] {
  if (agent.presence_state === "offline") {
    return [{ label: "Offline", tone: "rose" }];
  }

  const reasons: AgentAttentionReason[] = [];

  if (agent.runtime.degraded) {
    reasons.push({ label: "Runtime degraded", tone: "amber" });
  }

  if (!agent.runtime.accepting_new_connections) {
    reasons.push({ label: "Admissions paused", tone: "amber" });
  }

  if (agent.runtime.dc_coverage_pct === 0) {
    reasons.push({ label: "No DC coverage", tone: "rose" });
  } else if (agent.runtime.dc_coverage_pct > 0 && agent.runtime.dc_coverage_pct < 100) {
    reasons.push({ label: "Reduced DC coverage", tone: "amber" });
  }

  if (agent.runtime.total_upstreams > 0 && agent.runtime.healthy_upstreams < agent.runtime.total_upstreams) {
    reasons.push({ label: "Upstreams degraded", tone: "amber" });
  }

  return reasons;
}

export function buildAgentDCCoverageStage(agent: Agent): 0 | 33 | 66 | 100 {
  return coverageToStage(agent.runtime.dc_coverage_pct);
}

export function buildAgentDCSegments(agent: Agent): AgentDCSegment[] {
  return [...agent.runtime.dcs]
    .sort((left, right) => left.dc - right.dc)
    .map((dc) => {
      const stage = coverageToStage(dc.coverage_pct);
      return {
        dc: dc.dc,
        label: `DC${dc.dc}`,
        stage,
        stateLabel: stageToLabel(stage),
        coveragePct: dc.coverage_pct
      };
    });
}

export function buildAgentDCIssueSummary(agent: Agent): AgentDCIssueSummary {
  const segments = buildAgentDCSegments(agent);
  const issues = segments
    .filter((segment) => segment.stage < 100)
    .sort((left, right) => issueSeverityScore(right) - issueSeverityScore(left) || left.dc - right.dc)
    .map((segment) => issueFromSegment(segment));

  return {
    totalCount: segments.length,
    okCount: segments.length - issues.length,
    issueCount: issues.length,
    issues
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

function coverageToStage(coverage: number): 0 | 33 | 66 | 100 {
  if (coverage >= 99.5) {
    return 100;
  }

  if (coverage >= 66) {
    return 66;
  }

  if (coverage > 0) {
    return 33;
  }

  return 0;
}

function stageToLabel(stage: 0 | 33 | 66 | 100): "Full" | "Reduced" | "Limited" | "Down" {
  switch (stage) {
    case 100:
      return "Full";
    case 66:
      return "Reduced";
    case 33:
      return "Limited";
    default:
      return "Down";
  }
}

function stageToTone(stage: 0 | 33 | 66 | 100): "sky" | "amber" | "rose" {
  switch (stage) {
    case 66:
      return "sky";
    case 33:
      return "amber";
    default:
      return "rose";
  }
}

function issueSeverityScore(segment: AgentDCSegment): number {
  switch (segment.stage) {
    case 0:
      return 3;
    case 33:
      return 2;
    case 66:
      return 1;
    default:
      return 0;
  }
}

function issueFromSegment(segment: AgentDCSegment): {
  dc: number;
  label: string;
  stateLabel: "Reduced" | "Limited" | "Down";
  tone: "sky" | "amber" | "rose";
} {
  switch (segment.stage) {
    case 66:
      return {
        dc: segment.dc,
        label: `DC${segment.dc} reduced`,
        stateLabel: "Reduced",
        tone: "sky"
      };
    case 33:
      return {
        dc: segment.dc,
        label: `DC${segment.dc} limited`,
        stateLabel: "Limited",
        tone: "amber"
      };
    default:
      return {
        dc: segment.dc,
        label: `DC${segment.dc} down`,
        stateLabel: "Down",
        tone: "rose"
      };
  }
}
