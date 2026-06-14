/**
 * Localize the backend's English node-status reason strings
 * (internal/controlplane/telemetry/projections.go: SeverityAndReason).
 * SEAM: these literals must mirror the Go side; unknown/composite strings
 * (e.g. the ME→Direct fallback wrappers) fall through to verbatim.
 * Keys are relative to the `common` i18n namespace (`reason.*`).
 */
const REASON_KEYS: Record<string, string> = {
  "Agent heartbeat is offline": "reason.offline",
  "Telemetry is stale": "reason.stale",
  "Telemt API is read-only": "reason.readOnly",
  "Admission is closed": "reason.admissionClosed",
  "Startup is still in progress": "reason.startup",
  "no reachable DCs": "reason.noDcs",
  "DC coverage is degraded": "reason.dcDegraded",
  "ME runtime is degraded": "reason.meDegraded",
  "ME pool unavailable, traffic stopped": "reason.meDown",
  "upstream DC connect failing": "reason.upstreamFailing",
  "degraded DC connectivity": "reason.dcConnDegraded",
  "no upstreams configured": "reason.noUpstreams",
  "all upstreams down": "reason.allUpstreamsDown",
  "some upstreams unhealthy": "reason.someUpstreamsUnhealthy",
};

const TELEMT_PREFIX = "Telemt API unreachable since ";

/**
 * @param reason raw backend reason
 * @param t translator bound to the `common` namespace (resolves "reason.*")
 */
export function localizeReason(reason: string, t: (key: string) => string): string {
  const trimmed = reason.trim();
  if (trimmed === "") return "";
  const key = REASON_KEYS[trimmed];
  if (key) return t(key);
  if (trimmed.startsWith(TELEMT_PREFIX)) {
    return `${t("reason.telemtUnreachable")} ${trimmed.slice(TELEMT_PREFIX.length)}`;
  }
  return reason;
}
