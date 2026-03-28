import type { Agent, RuntimeEvent, TelemetryServerDetailResponse, TelemetryServerSummary } from "../../lib/api";

type DetailTone = "good" | "warn" | "bad" | "accent";

export interface ServerDetailViewModel {
  header: {
    nameText: string;
    statusText: string;
    statusTone: Exclude<DetailTone, "accent">;
    groupText: string;
    versionText: string;
    lastSeenText: string;
    readOnlyText: string;
    certificateRecoveryText: string;
    reasonText: string;
    freshnessText: string;
    detailBoostText: string;
  };
  overviewStats: Array<{
    label: string;
    valueText: string;
    secondaryText: string;
  }>;
  runtimeProgressCards: Array<{
    label: string;
    valueText: string;
    secondaryText: string;
    progressPct: number;
  }>;
  runtimeFlags: Array<{
    label: string;
    valueText: string;
    secondaryText: string;
  }>;
  dcRows: Array<{
    id: string;
    dcText: string;
    statusText: string;
    statusTone: Exclude<DetailTone, "accent">;
    rttText: string;
    coverageText: string;
    writersText: string;
    endpointsText: string;
    loadText: string;
  }>;
  connectionStats: Array<{
    label: string;
    valueText: string;
    secondaryText: string;
  }>;
  connectionMeta: Array<{
    label: string;
    valueText: string;
  }>;
  upstreamSummaryText: string;
  upstreamRows: Array<{
    id: string;
    routeKindText: string;
    addressText: string;
    healthText: string;
    healthTone: Exclude<DetailTone, "accent">;
    failsText: string;
    latencyText: string;
  }>;
  recentEventItems: Array<{
    id: string;
    text: string;
    time: string;
    status: DetailTone;
  }>;
  diagnosticsRows: Array<{
    label: string;
    valueText: string;
  }>;
  securityRows: Array<{
    label: string;
    valueText: string;
  }>;
  meDiagnosticsRows: Array<{
    label: string;
    valueText: string;
  }>;
  routingRows: Array<{
    label: string;
    valueText: string;
  }>;
  whitelistEntries: string[];
  diagnosticsStateText: string;
  securityStateText: string;
  meDiagnosticsStateText: string;
}

const integerFormatter = new Intl.NumberFormat("en-US");
const shortMonths = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export function buildServerDetailViewModel(
  input: TelemetryServerSummary | Agent,
  options?: { nowMs?: number; detail?: TelemetryServerDetailResponse }
): ServerDetailViewModel {
  const summary = "agent" in input ? input : {
    agent: input,
    severity: input.presence_state === "offline" ? "bad" : input.presence_state === "degraded" || input.runtime?.degraded ? "warn" : "good",
    reason: input.presence_state === "offline"
      ? "Agent heartbeat is offline"
      : input.runtime?.degraded
        ? "Runtime is degraded"
        : "Node is ready",
    runtime_freshness: {
      state: "fresh",
      observed_at_unix: input.last_seen_at ? Math.floor(Date.parse(input.last_seen_at) / 1000) : 0,
    },
    detail_boost: {
      active: false,
      expires_at_unix: 0,
      remaining_seconds: 0,
    },
  };
  const agent = summary.agent;
  const nowMs = options?.nowMs ?? Date.now();
  const detail = options?.detail;
  const status = resolveServerStatus(agent);
  const runtime = agent.runtime;
  const healthyUpstreams = runtime?.healthy_upstreams ?? 0;
  const totalUpstreams = runtime?.total_upstreams ?? 0;
  const diagnosticsRows = buildDiagnosticsRows(detail);
  const securityRows = buildSecurityRows(detail);
  const meDiagnosticsRows = buildMEDiagnosticsRows(detail);
  const routingRows = buildRoutingRows(detail);
  const mePool = asRecord(detail?.diagnostics?.me_pool);
  const meDiagnosticsStateText = buildSectionStateText(
    mePool.enabled === false ? "disabled" : detail?.diagnostics?.state,
    stringValue(mePool.reason) !== "—" ? stringValue(mePool.reason) : detail?.diagnostics?.state_reason
  );

  return {
    header: {
      nameText: agent.node_name,
      statusText: status.text,
      statusTone: status.tone,
      groupText: agent.fleet_group_id || "Ungrouped",
      versionText: agent.version || "—",
      lastSeenText: formatDateTime(agent.last_seen_at),
      readOnlyText: agent.read_only ? "Read-only" : "Writable",
      certificateRecoveryText: formatCertificateRecovery(agent.certificate_recovery),
      reasonText: summary.reason,
      freshnessText: humanizeToken(summary.runtime_freshness.state || "unknown"),
      detailBoostText: summary.detail_boost.active ? "Boost active" : "Boost off",
    },
    overviewStats: [
      {
        label: "Active Users",
        valueText: formatInteger(runtime?.active_users ?? 0),
        secondaryText: "Reported edge users",
      },
      {
        label: "Current Connections",
        valueText: formatInteger(runtime?.current_connections ?? 0),
        secondaryText: `${formatInteger(runtime?.current_connections_me ?? 0)} ME, ${formatInteger(runtime?.current_connections_direct ?? 0)} direct`,
      },
      {
        label: "DC Coverage",
        valueText: `${Math.round(runtime?.dc_coverage_pct ?? 0)}%`,
        secondaryText: buildDcCoverageSecondary(runtime?.dcs ?? []),
      },
      {
        label: "Healthy Upstreams",
        valueText: `${formatInteger(healthyUpstreams)} / ${formatInteger(totalUpstreams)}`,
        secondaryText: totalUpstreams === 0
          ? "No upstream routes configured"
          : totalUpstreams > healthyUpstreams
            ? `${formatInteger(totalUpstreams - healthyUpstreams)} degraded paths`
            : "All configured routes healthy",
      },
      {
        label: "Accepting New Connections",
        valueText: formatYesNo(runtime?.accepting_new_connections ?? false),
        secondaryText: runtime?.accepting_new_connections ? "Admission gates open" : "Admission gates closed",
      },
      {
        label: "Transport Mode",
        valueText: humanizeToken(runtime?.transport_mode || "unknown"),
        secondaryText: runtime?.me2dc_fallback_enabled ? "Fallback still enabled" : "Fallback disabled",
      },
    ],
    runtimeProgressCards: [
      {
        label: "Startup Status",
        valueText: humanizeToken(runtime?.startup_status || "unknown"),
        secondaryText: humanizeToken(runtime?.startup_stage || "unknown"),
        progressPct: normalizeProgress(runtime?.startup_progress_pct ?? 0),
      },
      {
        label: "Initialization",
        valueText: humanizeToken(runtime?.initialization_status || "unknown"),
        secondaryText: humanizeToken(runtime?.initialization_stage || "unknown"),
        progressPct: normalizeProgress(runtime?.initialization_progress_pct ?? 0),
      },
      {
        label: "Admission Gates",
        valueText: runtime?.accepting_new_connections ? "Open" : "Closed",
        secondaryText: runtime?.accepting_new_connections ? "new sessions allowed" : "new sessions blocked",
        progressPct: runtime?.accepting_new_connections ? 100 : 0,
      },
    ],
    runtimeFlags: [
      {
        label: "Accepting New Connections",
        valueText: formatYesNo(runtime?.accepting_new_connections ?? false),
        secondaryText: String(Boolean(runtime?.accepting_new_connections)),
      },
      {
        label: "ME Runtime Ready",
        valueText: formatYesNo(runtime?.me_runtime_ready ?? false),
        secondaryText: String(Boolean(runtime?.me_runtime_ready)),
      },
      {
        label: "me2dc Fallback Enabled",
        valueText: formatYesNo(runtime?.me2dc_fallback_enabled ?? false),
        secondaryText: String(Boolean(runtime?.me2dc_fallback_enabled)),
      },
      {
        label: "Use Middle Proxy",
        valueText: formatYesNo(runtime?.use_middle_proxy ?? false),
        secondaryText: String(Boolean(runtime?.use_middle_proxy)),
      },
    ],
    dcRows: [...(runtime?.dcs ?? [])]
      .sort(compareDcRows)
      .map((dc) => {
        const tone = resolveDcTone(dc.coverage_pct ?? 0);
        return {
          id: `dc-${dc.dc}`,
          dcText: String(dc.dc),
          statusText: tone === "good" ? "Healthy" : tone === "warn" ? "Partial" : "Down",
          statusTone: tone,
          rttText: dc.rtt_ms > 0 ? `${Math.round(dc.rtt_ms)} ms` : "—",
          coverageText: `${Math.round(dc.coverage_pct ?? 0)}%`,
          writersText: `${formatInteger(dc.alive_writers ?? 0)} / ${formatInteger(dc.required_writers ?? 0)}`,
          endpointsText: `${formatInteger(dc.available_endpoints ?? 0)} available`,
          loadText: formatLoad(dc.load),
        };
      }),
    connectionStats: [
      {
        label: "Current Connections",
        valueText: formatInteger(runtime?.current_connections ?? 0),
        secondaryText: "Reported active sockets",
      },
      {
        label: "ME Connections",
        valueText: formatInteger(runtime?.current_connections_me ?? 0),
        secondaryText: "Reported through ME transport",
      },
      {
        label: "Direct Connections",
        valueText: formatInteger(runtime?.current_connections_direct ?? 0),
        secondaryText: "Reported direct sessions",
      },
      {
        label: "Active Users",
        valueText: formatInteger(runtime?.active_users ?? 0),
        secondaryText: "Reported unique users",
      },
    ],
    connectionMeta: [
      { label: "Connections Total", valueText: formatInteger(runtime?.connections_total ?? 0) },
      { label: "Bad Connections", valueText: formatInteger(runtime?.connections_bad_total ?? 0) },
      { label: "Handshake Timeouts", valueText: formatInteger(runtime?.handshake_timeouts_total ?? 0) },
      { label: "Configured Users", valueText: formatInteger(runtime?.configured_users ?? 0) },
    ],
    upstreamSummaryText: `${formatInteger(healthyUpstreams)} of ${formatInteger(totalUpstreams)} upstreams healthy`,
    upstreamRows: [...(runtime?.upstreams ?? [])]
      .sort(compareUpstreamRows)
      .map((upstream) => ({
        id: `upstream-${upstream.upstream_id}`,
        routeKindText: humanizeToken(upstream.route_kind || "unknown"),
        addressText: upstream.address || "—",
        healthText: upstream.healthy ? "Healthy" : "Unhealthy",
        healthTone: upstream.healthy ? "good" : "bad",
        failsText: formatInteger(upstream.fails ?? 0),
        latencyText: upstream.effective_latency_ms > 0 ? `${Math.round(upstream.effective_latency_ms)} ms` : "—",
      })),
    recentEventItems: [...(runtime?.recent_events ?? [])]
      .sort((left, right) => {
        if (right.timestamp_unix !== left.timestamp_unix) {
          return right.timestamp_unix - left.timestamp_unix;
        }

        return right.sequence - left.sequence;
      })
      .map((event) => ({
        id: `${agent.id}-${event.sequence}-${event.timestamp_unix}`,
        text: event.context || humanizeToken(event.event_type || "unknown"),
        time: formatRelativeTimestamp(event, nowMs),
        status: mapEventTone(event.event_type || ""),
      })),
    diagnosticsRows,
    securityRows,
    meDiagnosticsRows,
    routingRows,
    whitelistEntries: detail?.security_inventory?.entries ?? [],
    diagnosticsStateText: buildSectionStateText(detail?.diagnostics?.state, detail?.diagnostics?.state_reason),
    securityStateText: buildSectionStateText(detail?.security_inventory?.state, detail?.security_inventory?.state_reason),
    meDiagnosticsStateText,
  };
}

function resolveServerStatus(agent: Agent): {
  text: "Online" | "Degraded" | "Offline";
  tone: "good" | "warn" | "bad";
} {
  if (agent.presence_state === "offline") {
    return { text: "Offline", tone: "bad" };
  }

  if (agent.presence_state === "degraded") {
    return { text: "Degraded", tone: "warn" };
  }

  return { text: "Online", tone: "good" };
}

function buildDcCoverageSecondary(dcs: Agent["runtime"]["dcs"]): string {
  if (dcs.length === 0) {
    return "No DC data";
  }

  const healthyCount = dcs.filter((dc) => (dc.coverage_pct ?? 0) >= 99.5).length;
  return `${healthyCount} of ${dcs.length} DCs healthy`;
}

function normalizeProgress(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }

  return Math.min(100, Math.max(0, Math.round(value)));
}

function formatInteger(value: number): string {
  return integerFormatter.format(Number.isFinite(value) ? value : 0);
}

function formatLoad(value: number): string {
  if (!Number.isFinite(value)) {
    return "—";
  }

  return value.toFixed(2);
}

function formatYesNo(value: boolean): "Yes" | "No" {
  return value ? "Yes" : "No";
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

function resolveDcTone(coveragePct: number): "good" | "warn" | "bad" {
  if (coveragePct >= 99.5) {
    return "good";
  }

  if (coveragePct > 0) {
    return "warn";
  }

  return "bad";
}

function compareDcRows(
  left: Agent["runtime"]["dcs"][number],
  right: Agent["runtime"]["dcs"][number]
): number {
  const severityDelta = getDcSeverity(right.coverage_pct ?? 0) - getDcSeverity(left.coverage_pct ?? 0);
  if (severityDelta !== 0) {
    return severityDelta;
  }

  return left.dc - right.dc;
}

function getDcSeverity(coveragePct: number): number {
  if (coveragePct <= 0) {
    return 3;
  }

  if (coveragePct < 99.5) {
    return 2;
  }

  return 1;
}

function compareUpstreamRows(
  left: Agent["runtime"]["upstreams"][number],
  right: Agent["runtime"]["upstreams"][number]
): number {
  if (left.healthy !== right.healthy) {
    return left.healthy ? 1 : -1;
  }

  if ((left.fails ?? 0) !== (right.fails ?? 0)) {
    return (left.fails ?? 0) - (right.fails ?? 0);
  }

  if ((left.effective_latency_ms ?? 0) !== (right.effective_latency_ms ?? 0)) {
    return (left.effective_latency_ms ?? 0) - (right.effective_latency_ms ?? 0);
  }

  return (left.address || "").localeCompare(right.address || "", "en", { sensitivity: "base" });
}

function formatDateTime(value: string): string {
  const date = new Date(value);
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

function formatCertificateRecovery(recovery: Agent["certificate_recovery"] | undefined): string {
  if (!recovery) {
    return "Not allowed";
  }

  switch (recovery.status) {
    case "allowed":
      return `Allowed until ${formatUnixDateTime(recovery.expires_at_unix)}`;
    case "used":
      return "Used";
    case "revoked":
      return "Revoked";
    case "expired":
      return "Expired";
    default:
      return "Not allowed";
  }
}

function formatUnixDateTime(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "Unknown";
  }

  return formatDateTime(new Date(value * 1000).toISOString());
}

function formatRelativeTimestamp(event: RuntimeEvent, nowMs: number): string {
  const eventTimestamp = event.timestamp_unix * 1000;
  if (!Number.isFinite(eventTimestamp) || eventTimestamp <= 0) {
    return "unknown";
  }

  const diffMs = Math.max(0, nowMs - eventTimestamp);
  const diffMinutes = Math.floor(diffMs / 60_000);

  if (diffMinutes < 1) {
    return "just now";
  }

  if (diffMinutes < 60) {
    return `${diffMinutes} min ago`;
  }

  const diffHours = Math.floor(diffMinutes / 60);
  if (diffHours < 24) {
    return `${diffHours} hr ago`;
  }

  const diffDays = Math.floor(diffHours / 24);
  return `${diffDays} d ago`;
}

function mapEventTone(eventType: string): DetailTone {
  const normalized = eventType.toLowerCase();

  if (
    normalized.includes("error") ||
    normalized.includes("fail") ||
    normalized.includes("disconnect") ||
    normalized.includes("offline") ||
    normalized.includes("crash") ||
    normalized.includes("down")
  ) {
    return "bad";
  }

  if (
    normalized.includes("warn") ||
    normalized.includes("timeout") ||
    normalized.includes("retry") ||
    normalized.includes("slow") ||
    normalized.includes("degrad")
  ) {
    return "warn";
  }

  if (
    normalized.includes("connect") ||
    normalized.includes("online") ||
    normalized.includes("join") ||
    normalized.includes("register") ||
    normalized.includes("recover") ||
    normalized.includes("ready")
  ) {
    return "good";
  }

  return "accent";
}

function buildDiagnosticsRows(detail: TelemetryServerDetailResponse | undefined): Array<{ label: string; valueText: string }> {
  if (!detail) {
    return [];
  }

  const systemInfo = detail.diagnostics?.system_info ?? {};
  const limits = detail.diagnostics?.effective_limits ?? {};
  const userIPPolicy = asRecord(limits.user_ip_policy);
  const middleProxy = asRecord(limits.middle_proxy);

  return [
    { label: "Telemt Version", valueText: stringValue(systemInfo.version) },
    { label: "Target", valueText: compactJoin([stringValue(systemInfo.target_os), stringValue(systemInfo.target_arch)], " / ") },
    { label: "Build Profile", valueText: stringValue(systemInfo.build_profile) },
    { label: "Git Commit", valueText: stringValue(systemInfo.git_commit) },
    { label: "Config Hash", valueText: stringValue(systemInfo.config_hash) },
    { label: "Config Reloads", valueText: numberValue(systemInfo.config_reload_count) },
    { label: "Update Every", valueText: secondsValue(limits.update_every_secs) },
    { label: "ME Reinit", valueText: secondsValue(limits.me_reinit_every_secs) },
    { label: "Force Close", valueText: secondsValue(limits.me_pool_force_close_secs) },
    { label: "IP Policy", valueText: stringValue(userIPPolicy.mode) },
    { label: "IP Window", valueText: secondsValue(userIPPolicy.window_secs) },
    { label: "Floor Mode", valueText: stringValue(middleProxy.floor_mode) },
    { label: "Writer Pick Mode", valueText: stringValue(middleProxy.writer_pick_mode) },
  ].filter((row) => row.valueText !== "—");
}

function buildSecurityRows(detail: TelemetryServerDetailResponse | undefined): Array<{ label: string; valueText: string }> {
  if (!detail) {
    return [];
  }

  const posture = detail.diagnostics?.security_posture ?? {};
  return [
    { label: "API Read-Only", valueText: boolValue(posture.api_read_only) },
    { label: "Whitelist Enabled", valueText: boolValue(posture.api_whitelist_enabled) },
    { label: "Whitelist Entries", valueText: numberValue(posture.api_whitelist_entries) },
    { label: "Auth Header", valueText: boolValue(posture.api_auth_header_enabled) },
    { label: "Proxy Protocol", valueText: boolValue(posture.proxy_protocol_enabled) },
    { label: "Log Level", valueText: stringValue(posture.log_level) },
    { label: "Telemetry Core", valueText: boolValue(posture.telemetry_core_enabled) },
    { label: "Telemetry User", valueText: boolValue(posture.telemetry_user_enabled) },
    { label: "Telemetry ME", valueText: stringValue(posture.telemetry_me_level) },
  ].filter((row) => row.valueText !== "—");
}

function buildMEDiagnosticsRows(detail: TelemetryServerDetailResponse | undefined): Array<{ label: string; valueText: string }> {
  if (!detail) {
    return [];
  }

  const mePool = asRecord(detail.diagnostics?.me_pool);
  const mePoolData = asRecord(mePool.data);
  return [
    { label: "Active Generation", valueText: numberValue(mePoolData.active_generation) },
    { label: "Warm Generation", valueText: numberValue(mePoolData.warm_generation) },
    { label: "Pending Hardswap", valueText: numberValue(mePoolData.pending_hardswap_generation) },
  ].filter((row) => row.valueText !== "—");
}

function buildRoutingRows(detail: TelemetryServerDetailResponse | undefined): Array<{ label: string; valueText: string }> {
  if (!detail) {
    return [];
  }

  const minimalAll = asRecord(detail.diagnostics?.minimal_all);
  const minimalData = asRecord(minimalAll.data);
  const networkPath = Array.isArray(minimalData.network_path) ? minimalData.network_path : [];
  return networkPath
    .map((row) => asRecord(row))
    .map((row) => ({
      label: `DC ${numberValue(row.dc) === "—" ? "?" : numberValue(row.dc)} Path`,
      valueText: stringValue(row.selected_ip),
    }))
    .filter((row) => row.valueText !== "—");
}

function buildSectionStateText(state: string | undefined, reason: string | undefined): string {
  if (!state) {
    return "No diagnostics data yet";
  }
  if (!reason) {
    return humanizeToken(state);
  }
  return `${humanizeToken(state)}: ${humanizeToken(reason)}`;
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  return value as Record<string, unknown>;
}

function stringValue(value: unknown): string {
  if (typeof value === "string" && value.trim() !== "") {
    return value;
  }
  return "—";
}

function numberValue(value: unknown): string {
  if (typeof value === "number" && Number.isFinite(value)) {
    return formatInteger(value);
  }
  return "—";
}

function secondsValue(value: unknown): string {
  if (typeof value === "number" && Number.isFinite(value)) {
    return `${formatInteger(value)}s`;
  }
  return "—";
}

function boolValue(value: unknown): string {
  if (typeof value === "boolean") {
    return value ? "Yes" : "No";
  }
  return "—";
}

function compactJoin(values: string[], separator: string): string {
  const filtered = values.filter((value) => value && value !== "—");
  if (filtered.length === 0) {
    return "—";
  }
  return filtered.join(separator);
}
