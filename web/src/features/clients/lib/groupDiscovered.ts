import type { DiscoveredClientItem } from "@/shared/api/types-pages/pages";

export interface DiscoveredGroup {
  /** Canonical id used as React key + as the "representative" record id. */
  key: string;
  clientName: string;
  /** All raw discovered-record ids this group folds in. */
  ids: string[];
  /** node_names this logical client shows up on. */
  discoveredOn: string[];
  status: "pending_review" | "adopted" | "ignored" | "mixed";
  currentConnections: number;
  activeUniqueIps: number;
  totalOctets: number;
  maxTcpConns: number;
  maxUniqueIps: number;
  dataQuotaBytes: number;
  expiration: string;
  discoveredAtUnix: number;
  hasConflict: boolean;
  /** True when the group is a `same_name_different_secrets` singleton. */
  hasNameConflict: boolean;
}

export interface DiscoveredGroupCounts {
  all: number;
  pending: number;
  adopted: number;
  ignored: number;
  conflicts: number;
}

/**
 * Merge raw discovered-client records into logical groups by clientName.
 * Records with a `same_name_different_secrets` conflict stay as singletons
 * so two genuinely different clients aren't silently merged.
 *
 * Stop-gap for backend-followup #6 (server-side dedup on
 * `(client_name, secret_hash)`). Once the backend starts returning one
 * row per logical client with `discovered_on: string[]`, this helper
 * becomes a thin mapper and the group semantics move server-side.
 */
export function groupDiscovered(items: DiscoveredClientItem[]): DiscoveredGroup[] {
  const buckets = new Map<string, DiscoveredClientItem[]>();
  const singletons: DiscoveredClientItem[] = [];
  for (const it of items) {
    const nameConflict =
      it.conflicts?.some((c) => c.type === "same_name_different_secrets") ?? false;
    if (nameConflict) {
      singletons.push(it);
      continue;
    }
    const list = buckets.get(it.clientName) ?? [];
    list.push(it);
    buckets.set(it.clientName, list);
  }

  const out: DiscoveredGroup[] = [];
  for (const [name, list] of buckets) {
    const statuses = new Set(list.map((i) => i.status));
    const status: DiscoveredGroup["status"] =
      statuses.size === 1 ? (list[0]!.status as DiscoveredGroup["status"]) : "mixed";
    const sample = list[0]!;
    out.push({
      key: sample.id,
      clientName: name,
      ids: list.map((i) => i.id),
      discoveredOn: Array.from(new Set(list.map((i) => i.nodeName).filter(Boolean))),
      status,
      currentConnections: list.reduce((a, i) => a + i.currentConnections, 0),
      activeUniqueIps: list.reduce((a, i) => a + i.activeUniqueIps, 0),
      totalOctets: list.reduce((a, i) => a + i.totalOctets, 0),
      maxTcpConns: sample.maxTcpConns,
      maxUniqueIps: sample.maxUniqueIps,
      dataQuotaBytes: sample.dataQuotaBytes,
      expiration: sample.expiration,
      discoveredAtUnix: Math.min(...list.map((i) => i.discoveredAtUnix || Infinity)),
      hasConflict: list.some((i) => (i.conflicts ?? []).length > 0),
      hasNameConflict: false,
    });
  }
  for (const it of singletons) {
    out.push({
      key: it.id,
      clientName: it.clientName,
      ids: [it.id],
      discoveredOn: it.nodeName ? [it.nodeName] : [],
      status: it.status as DiscoveredGroup["status"],
      currentConnections: it.currentConnections,
      activeUniqueIps: it.activeUniqueIps,
      totalOctets: it.totalOctets,
      maxTcpConns: it.maxTcpConns,
      maxUniqueIps: it.maxUniqueIps,
      dataQuotaBytes: it.dataQuotaBytes,
      expiration: it.expiration,
      discoveredAtUnix: it.discoveredAtUnix,
      hasConflict: true,
      hasNameConflict: true,
    });
  }
  return out;
}

/** Derived group-level counters used by the page, the dashboard banner
 *  and the clients list — so every surface reports the logical-client
 *  number, not the raw-record one. */
export function countDiscoveredGroups(
  items: DiscoveredClientItem[],
): DiscoveredGroupCounts {
  const groups = groupDiscovered(items);
  let pending = 0,
    adopted = 0,
    ignored = 0,
    conflicts = 0;
  for (const g of groups) {
    if (g.status === "pending_review") pending++;
    else if (g.status === "adopted") adopted++;
    else if (g.status === "ignored") ignored++;
    if (g.hasConflict) conflicts++;
  }
  return { all: groups.length, pending, adopted, ignored, conflicts };
}
