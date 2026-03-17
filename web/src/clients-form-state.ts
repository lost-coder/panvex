import type { Client, ClientInput } from "./lib/api";

export type ClientAdTagMode = "auto" | "manual";
export type AssignmentOption = {
  id: string;
  label: string;
};

export type AssignmentAgent = {
  id: string;
  fleet_group_id: string;
};

export type AssignmentSummary = {
  fleetGroupCount: number;
  explicitNodeCount: number;
  coveredNodeCount: number;
};

export type ClientFormState = {
  name: string;
  enabled: boolean;
  userADTag: string;
  adTagMode: ClientAdTagMode;
  maxTCPConns: string;
  maxUniqueIPs: string;
  dataQuotaBytes: string;
  expirationRFC3339: string;
  fleetGroupIDs: string[];
  agentIDs: string[];
};

export type ClientFormErrors = Partial<Record<"name" | "assignments" | "userADTag", string>>;

export function emptyClientForm(): ClientFormState {
  return {
    name: "",
    enabled: true,
    userADTag: "",
    adTagMode: "auto",
    maxTCPConns: "0",
    maxUniqueIPs: "0",
    dataQuotaBytes: "0",
    expirationRFC3339: "",
    fleetGroupIDs: [],
    agentIDs: []
  };
}

export function clientToForm(client: Client): ClientFormState {
  return {
    name: client.name,
    enabled: client.enabled,
    userADTag: client.user_ad_tag,
    adTagMode: "manual",
    maxTCPConns: String(client.max_tcp_conns),
    maxUniqueIPs: String(client.max_unique_ips),
    dataQuotaBytes: String(client.data_quota_bytes),
    expirationRFC3339: client.expiration_rfc3339,
    fleetGroupIDs: client.fleet_group_ids,
    agentIDs: client.agent_ids
  };
}

export function validateClientForm(form: ClientFormState): ClientFormErrors {
  const errors: ClientFormErrors = {};

  if (form.name.trim() === "") {
    errors.name = "Client name is required.";
  }

  if (form.fleetGroupIDs.length === 0 && form.agentIDs.length === 0) {
    errors.assignments = "Select at least one fleet group or node.";
  }

  if (form.adTagMode === "manual") {
    const adTag = form.userADTag.trim();
    if (!/^[0-9a-fA-F]{32}$/.test(adTag)) {
      errors.userADTag = "Ad tag must contain exactly 32 hexadecimal characters.";
    }
  }

  return errors;
}

export function formToClientInput(form: ClientFormState): ClientInput {
  return {
    name: form.name.trim(),
    enabled: form.enabled,
    user_ad_tag: form.adTagMode === "manual" ? form.userADTag.trim() : "",
    max_tcp_conns: toNumber(form.maxTCPConns),
    max_unique_ips: toNumber(form.maxUniqueIPs),
    data_quota_bytes: toNumber(form.dataQuotaBytes),
    expiration_rfc3339: form.expirationRFC3339.trim(),
    fleet_group_ids: form.fleetGroupIDs,
    agent_ids: form.agentIDs
  };
}

export function filterAssignmentOptions(options: AssignmentOption[], query: string, selected: string[]): AssignmentOption[] {
  const normalizedQuery = query.trim().toLowerCase();

  return options.filter((option) => {
    if (selected.includes(option.id)) {
      return false;
    }

    if (normalizedQuery === "") {
      return true;
    }

    return option.label.toLowerCase().includes(normalizedQuery);
  });
}

export function summarizeAssignmentSelection(options: AssignmentOption[], selected: string[]): string {
  const selectedOptions = options.filter((option) => selected.includes(option.id));

  if (selectedOptions.length === 0) {
    return "No selections";
  }

  const visibleLabels = selectedOptions.slice(0, 2).map((option) => option.label);
  const hiddenCount = selectedOptions.length - visibleLabels.length;

  if (hiddenCount <= 0) {
    return visibleLabels.join(", ");
  }

  return `${visibleLabels.join(", ")} +${hiddenCount} more`;
}

export function summarizeAssignments(form: ClientFormState, agents: AssignmentAgent[]): AssignmentSummary {
  const coveredAgentIDs = new Set<string>();

  for (const agent of agents) {
    if (form.fleetGroupIDs.includes(agent.fleet_group_id) || form.agentIDs.includes(agent.id)) {
      coveredAgentIDs.add(agent.id);
    }
  }

  return {
    fleetGroupCount: form.fleetGroupIDs.length,
    explicitNodeCount: form.agentIDs.length,
    coveredNodeCount: coveredAgentIDs.size
  };
}

function toNumber(value: string) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}
