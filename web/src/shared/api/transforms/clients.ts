import type { ClientListItem, ClientDetailPageProps, ClientFormData } from "@/shared/api/types-pages/pages";
import type {
  ClientListItem as ApiClientListItem,
  Client as ApiClient,
  ClientInput,
} from "../api";

const SECRET_PARAM_RE = /secret=([0-9a-fA-F]+)/;

/**
 * Categorize a list of Telemt connection links into TLS / Secure /
 * Classic buckets by inspecting the URL shape and secret prefix:
 *   * https://t.me/...                  → classic (Telegram-served)
 *   * tg://proxy?...&secret=ee...       → TLS (FakeTLS, ee + domain hex)
 *   * tg://proxy?...&secret=dd...       → secure (dd-prefixed)
 *   * tg://proxy?...&secret=<raw hex>   → classic
 *
 * The agent forwards every link Telemt returns (one per tls_domain ×
 * host); preserving them all here lets the panel render the full set
 * with copy buttons instead of a single "preferred" link.
 */
export function categorizeConnectionLinks(links: readonly string[]): { classic: string[]; secure: string[]; tls: string[] } {
  const out: { classic: string[]; secure: string[]; tls: string[] } = {
    classic: [],
    secure: [],
    tls: [],
  };
  for (const raw of links) {
    const item = raw?.trim();
    if (!item) continue;
    classifyLink(item, out);
  }
  return out;
}

function classifyLink(
  link: string,
  out: { classic: string[]; secure: string[]; tls: string[] },
): void {
  if (link.startsWith("https://t.me/")) {
    out.classic.push(link);
    return;
  }
  if (link.startsWith("tg://proxy")) {
    const match = SECRET_PARAM_RE.exec(link);
    const secret = match?.[1]?.toLowerCase() ?? "";
    if (secret.startsWith("ee")) {
      out.tls.push(link);
      return;
    }
    if (secret.startsWith("dd")) {
      out.secure.push(link);
      return;
    }
    out.classic.push(link);
    return;
  }
  out.secure.push(link);
}

export function transformClientList(
  raw: ApiClientListItem[]
): ClientListItem[] {
  return (raw ?? []).map((c) => ({
    id: c.id,
    name: c.name,
    enabled: c.enabled,
    assignedNodesCount: c.assigned_nodes_count,
    expirationRfc3339: c.expiration_rfc3339,
    trafficUsedBytes: c.traffic_used_bytes,
    uniqueIpsUsed: c.unique_ips_used,
    activeTcpConns: c.active_tcp_conns,
    dataQuotaBytes: c.data_quota_bytes,
    lastDeployStatus: c.last_deploy_status,
  }));
}

export function transformClientDetail(
  raw: ApiClient
): ClientDetailPageProps["client"] {
  return {
    id: raw.id,
    name: raw.name,
    enabled: raw.enabled,
    secret: raw.secret,
    userAdTag: raw.user_ad_tag,
    trafficUsedBytes: raw.traffic_used_bytes,
    uniqueIpsUsed: raw.unique_ips_used,
    activeTcpConns: raw.active_tcp_conns,
    maxTcpConns: raw.max_tcp_conns,
    maxUniqueIps: raw.max_unique_ips,
    dataQuotaBytes: raw.data_quota_bytes,
    expirationRfc3339: raw.expiration_rfc3339,
    fleetGroupIds: raw.fleet_group_ids ?? [],
    agentIds: raw.agent_ids ?? [],
    subscriptionUrl: raw.subscription_url,
    deployments: (raw.deployments ?? []).map((d) => ({
      agentId: d.agent_id,
      desiredOperation: d.desired_operation,
      status: d.status,
      lastError: d.last_error,
      links: categorizeConnectionLinks(d.connection_links),
      lastAppliedAtUnix: d.last_applied_at_unix,
      // Reset-quota Phase 1: API type carries these as optional so we
      // do not break callers that don't need them; the zod schema
      // upstream supplies a `.default(0)`, so a missing field on the
      // wire is observably "0" by the time it reaches the UI.
      quotaUsedBytes: d.quota_used_bytes ?? 0,
      quotaLastResetUnix: d.quota_last_reset_unix ?? 0,
      // Reset-quota Phase 3: panel-recorded last reset + drift flag
      // surfaced when the panel believes a reset happened but Telemt
      // is still reporting an older timestamp. Default both to safe
      // zero / false so older-backend responses parse cleanly.
      panelLastResetUnix: d.panel_last_reset_unix ?? 0,
      quotaResetDrift: d.quota_reset_drift ?? false,
    })),
  };
}

/**
 * Convert ClientFormData back to API ClientInput.
 *
 * Deployment targets (fleet_group_ids / agent_ids) come from the form
 * when the sheet supplied selectors — the form is the source of truth
 * for the user's current intent. Callers that edit a client without
 * surfacing the selectors (e.g. toggleEnabled on the detail page) pass
 * the existing assignments through the form payload instead.
 */
export function buildClientInput(form: ClientFormData, existing: ApiClient): ClientInput {
  return {
    name: form.name,
    enabled: existing.enabled,
    user_ad_tag: form.userAdTag,
    user_ad_tag_auto: form.userAdTagAuto,
    max_tcp_conns: form.maxTcpConns,
    max_unique_ips: form.maxUniqueIps,
    data_quota_bytes: form.dataQuotaBytes,
    expiration_rfc3339: form.expirationRfc3339,
    fleet_group_ids: form.fleetGroupIds,
    agent_ids: form.agentIds,
  };
}
