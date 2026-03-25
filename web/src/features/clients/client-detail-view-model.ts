import type { Client } from "../../lib/api";

type DetailTone = "good" | "warn" | "bad" | "accent";

export interface ClientDetailViewModel {
  header: {
    nameText: string;
    statusText: "Active" | "Idle" | "Disabled";
    statusTone: Exclude<DetailTone, "accent">;
    deploymentText: string;
    deploymentTone: DetailTone;
    metaItems: Array<{
      label: string;
      valueText: string;
    }>;
  };
  overviewStats: Array<{
    label: string;
    valueText: string;
    secondaryText: string;
  }>;
  identityItems: Array<{
    label: string;
    valueText: string;
  }>;
  identitySecret: {
    maskedText: string;
    revealedText: string;
  };
  usageItems: Array<{
    label: string;
    valueText: string;
  }>;
  limitItems: Array<{
    label: string;
    valueText: string;
  }>;
  assignmentSummaryText: string;
  assignmentGroups: string[];
  assignmentAgents: string[];
  deploymentRows: Array<{
    id: string;
    agentText: string;
    statusText: string;
    statusTone: DetailTone;
    desiredOperationText: string;
    lastAppliedText: string;
    linkText: string;
    errorText: string;
  }>;
}

const shortMonths = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const integerFormatter = new Intl.NumberFormat("en-US");

export function buildClientDetailViewModel(
  client: Client,
  options?: { nowMs?: number }
): ClientDetailViewModel {
  const nowMs = options?.nowMs ?? Date.now();
  const status = resolveClientStatus(client);
  const hasConfiguredAssignments = client.fleet_group_ids.length > 0 || client.agent_ids.length > 0;
  const assignmentSummaryText = buildAssignmentSummary(client);
  const deploymentSummary = buildDeploymentSummary(client.deployments, hasConfiguredAssignments);
  const targetsCount = client.deployments.length;
  const expiresPrimaryText = formatExpirationPrimary(client.expiration_rfc3339, nowMs);
  const expiresSecondaryText = formatExpirationSecondary(client.expiration_rfc3339);
  const quotaText = client.data_quota_bytes > 0
    ? `${formatBytes(client.traffic_used_bytes)} / ${formatBytes(client.data_quota_bytes)}`
    : "—";

  return {
    header: {
      nameText: client.name,
      statusText: status.text,
      statusTone: status.tone,
      deploymentText: deploymentSummary.text,
      deploymentTone: deploymentSummary.tone,
      metaItems: [
        { label: "Client ID", valueText: client.id },
        {
          label: "Targets",
          valueText: targetsCount > 0
            ? `${formatInteger(targetsCount)} rollout target${targetsCount === 1 ? "" : "s"}`
            : hasConfiguredAssignments
              ? assignmentSummaryText
              : "No rollout targets",
        },
        { label: "Expires", valueText: expiresSecondaryText === "—" ? "No expiration" : expiresSecondaryText },
      ],
    },
    overviewStats: [
      {
        label: "Traffic",
        valueText: formatBytes(client.traffic_used_bytes),
        secondaryText: "Current aggregated usage",
      },
      {
        label: "Active TCP",
        valueText: formatInteger(client.active_tcp_conns),
        secondaryText: "Reported active TCP sessions",
      },
      {
        label: "Assigned Nodes",
        valueText: targetsCount > 0 ? formatInteger(targetsCount) : hasConfiguredAssignments ? "Pending" : "0",
        secondaryText: targetsCount > 0
          ? "Current rollout targets"
          : hasConfiguredAssignments
            ? "Assignments configured, rollout targets not reported yet"
            : "No rollout targets configured",
      },
      {
        label: "Quota",
        valueText: quotaText,
        secondaryText: client.data_quota_bytes > 0 ? "Traffic used versus cap" : "No traffic cap configured",
      },
      {
        label: "Expires",
        valueText: expiresPrimaryText,
        secondaryText: expiresSecondaryText === "—" ? "No expiration configured" : expiresSecondaryText,
      },
      {
        label: "Deployment Health",
        valueText: deploymentSummary.text,
        secondaryText: deploymentSummary.secondaryText,
      },
    ],
    identityItems: [
      { label: "Name", valueText: client.name },
      { label: "Client ID", valueText: client.id },
      { label: "AD Tag", valueText: client.user_ad_tag || "—" },
      { label: "State", valueText: client.enabled ? "Enabled" : "Disabled" },
    ],
    identitySecret: {
      maskedText: maskSecret(client.secret),
      revealedText: client.secret || "—",
    },
    usageItems: [
      { label: "Traffic used", valueText: formatBytes(client.traffic_used_bytes) },
      { label: "Unique IPs", valueText: formatInteger(client.unique_ips_used) },
      { label: "Active TCP", valueText: formatInteger(client.active_tcp_conns) },
    ],
    limitItems: [
      { label: "Max TCP connections", valueText: formatLimit(client.max_tcp_conns) },
      { label: "Max unique IPs", valueText: formatLimit(client.max_unique_ips) },
      { label: "Traffic quota", valueText: client.data_quota_bytes > 0 ? formatBytes(client.data_quota_bytes) : "No quota" },
      { label: "Expiration", valueText: expiresSecondaryText === "—" ? "No expiration" : expiresSecondaryText },
    ],
    assignmentSummaryText,
    assignmentGroups: [...client.fleet_group_ids],
    assignmentAgents: [...client.agent_ids],
    deploymentRows: [...client.deployments]
      .sort(compareDeployments)
      .map((deployment) => ({
        id: `${client.id}-${deployment.agent_id}`,
        agentText: deployment.agent_id,
        statusText: humanizeToken(deployment.status || "unknown"),
        statusTone: resolveDeploymentTone(deployment.status),
        desiredOperationText: humanizeToken(deployment.desired_operation || "unknown"),
        lastAppliedText: deployment.last_applied_at_unix > 0
          ? formatDateTimeFromUnix(deployment.last_applied_at_unix)
          : "Not applied yet",
        linkText: deployment.connection_link || "—",
        errorText: deployment.last_error || "—",
      })),
  };
}

function resolveClientStatus(client: Client): {
  text: "Active" | "Idle" | "Disabled";
  tone: "good" | "warn" | "bad";
} {
  if (!client.enabled) {
    return { text: "Disabled", tone: "bad" };
  }

  if (client.active_tcp_conns > 0) {
    return { text: "Active", tone: "good" };
  }

  return { text: "Idle", tone: "warn" };
}

function buildDeploymentSummary(
  deployments: Client["deployments"],
  hasConfiguredAssignments: boolean
): {
  text: string;
  tone: DetailTone;
  secondaryText: string;
} {
  if (deployments.length === 0) {
    return {
      text: hasConfiguredAssignments ? "Targets pending materialization" : "No rollout targets",
      tone: hasConfiguredAssignments ? "warn" : "accent",
      secondaryText: hasConfiguredAssignments
        ? "Assignments configured, waiting for deployment rows"
        : "No rollout targets configured",
    };
  }

  let healthy = 0;
  let pending = 0;
  let failed = 0;
  let attention = 0;

  for (const deployment of deployments) {
    const tone = resolveDeploymentTone(deployment.status);
    if (tone === "good") {
      healthy++;
    } else if (tone === "warn") {
      pending++;
    } else if (tone === "bad") {
      failed++;
    } else {
      attention++;
    }
  }

  const parts: string[] = [];
  if (failed > 0) {
    parts.push(`${formatInteger(failed)} failed`);
  }
  if (pending > 0) {
    parts.push(`${formatInteger(pending)} pending`);
  }
  if (attention > 0) {
    parts.push(`${formatInteger(attention)} attention`);
  }

  if (parts.length > 0) {
    return {
      text: parts.join(", "),
      tone: failed > 0 ? "bad" : "warn",
      secondaryText: healthy > 0
        ? `${formatInteger(healthy)} healthy rollout${healthy === 1 ? "" : "s"}`
        : "No healthy rollouts yet",
    };
  }

  return {
    text: `${formatInteger(healthy)} healthy rollout${healthy === 1 ? "" : "s"}`,
    tone: "good",
    secondaryText: "All rollout targets healthy",
  };
}

function buildAssignmentSummary(client: Client): string {
  const groupsCount = client.fleet_group_ids.length;
  const agentsCount = client.agent_ids.length;

  if (groupsCount === 0 && agentsCount === 0) {
    return "No rollout targets configured";
  }

  const parts: string[] = [];
  if (groupsCount > 0) {
    parts.push(`${formatInteger(groupsCount)} fleet group${groupsCount === 1 ? "" : "s"}`);
  }
  if (agentsCount > 0) {
    parts.push(`${formatInteger(agentsCount)} explicit node${agentsCount === 1 ? "" : "s"}`);
  }

  return parts.join(", ");
}

function compareDeployments(
  left: Client["deployments"][number],
  right: Client["deployments"][number]
): number {
  const severityDelta = getDeploymentSeverity(right.status) - getDeploymentSeverity(left.status);
  if (severityDelta !== 0) {
    return severityDelta;
  }

  return compareLabels(left.agent_id, right.agent_id);
}

function getDeploymentSeverity(status: string): number {
  const tone = resolveDeploymentTone(status);
  if (tone === "bad") {
    return 4;
  }
  if (tone === "warn") {
    return 3;
  }
  if (tone === "accent") {
    return 2;
  }
  return 1;
}

function resolveDeploymentTone(status: string): DetailTone {
  const normalized = status.toLowerCase();

  if (normalized === "failed" || normalized === "error") {
    return "bad";
  }

  if (normalized === "queued" || normalized === "pending" || normalized === "running") {
    return "warn";
  }

  if (normalized === "succeeded" || normalized === "enabled") {
    return "good";
  }

  return "accent";
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }

  const units = ["B", "KB", "MB", "GB", "TB"];
  let amount = value;
  let unitIndex = 0;

  while (amount >= 1024 && unitIndex < units.length - 1) {
    amount /= 1024;
    unitIndex++;
  }

  const precision = amount >= 10 || unitIndex === 0 ? 0 : 1;
  return `${amount.toFixed(precision)} ${units[unitIndex]}`;
}

function formatInteger(value: number): string {
  return integerFormatter.format(Number.isFinite(value) ? value : 0);
}

function formatLimit(value: number): string {
  return value > 0 ? formatInteger(value) : "Unlimited";
}

function formatExpirationPrimary(value: string, nowMs: number): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return "No expiration";
  }

  const diffMs = timestamp - nowMs;
  const absDiffMs = Math.abs(diffMs);
  const diffMinutes = Math.floor(absDiffMs / 60_000);
  const diffHours = Math.floor(absDiffMs / 3_600_000);
  const diffDays = Math.floor(absDiffMs / 86_400_000);

  if (diffMs < 0) {
    if (diffHours < 1) {
      return `expired ${Math.max(1, diffMinutes)}m ago`;
    }
    if (diffDays < 1) {
      return `expired ${diffHours}h ago`;
    }
    return `expired ${diffDays}d ago`;
  }

  if (diffHours < 1) {
    return `in ${Math.max(1, diffMinutes)}m`;
  }
  if (diffDays < 1) {
    return `in ${diffHours}h`;
  }
  return `in ${diffDays}d`;
}

function formatExpirationSecondary(value: string): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return "—";
  }

  return formatDateLabel(new Date(timestamp));
}

function formatDateTimeFromUnix(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "Unknown";
  }

  return formatDateTime(new Date(value * 1000));
}

function formatDateTime(date: Date): string {
  if (Number.isNaN(date.getTime())) {
    return "Unknown";
  }

  const day = String(date.getUTCDate()).padStart(2, "0");
  const month = shortMonths[date.getUTCMonth()] ?? "—";
  const year = date.getUTCFullYear();
  const hours = String(date.getUTCHours()).padStart(2, "0");
  const minutes = String(date.getUTCMinutes()).padStart(2, "0");

  return `${day} ${month} ${year}, ${hours}:${minutes} UTC`;
}

function formatDateLabel(date: Date): string {
  if (Number.isNaN(date.getTime())) {
    return "—";
  }

  const month = shortMonths[date.getUTCMonth()] ?? "—";
  const day = String(date.getUTCDate()).padStart(2, "0");
  const year = date.getUTCFullYear();

  return `${month} ${day}, ${year}`;
}

function humanizeToken(value: string): string {
  if (!value) {
    return "Unknown";
  }

  return value
    .split(/[_-]+/g)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function maskSecret(value: string): string {
  if (!value) {
    return "—";
  }

  if (value.length <= 12) {
    return "••••••";
  }

  return `${value.slice(0, 9)}...${value.slice(-4)}`;
}

function compareLabels(left: string, right: string): number {
  return left.localeCompare(right, "en", { sensitivity: "base" });
}
