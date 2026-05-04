import { useQuery } from "@tanstack/react-query";
import type { JobListItem, AuditListItem } from "@/shared/api/types-pages/pages";
import { apiClient, type Job } from "@/shared/api/api";
import { useProfile } from "@/features/auth/hooks/useProfile";
import { clientsKeys } from "@/features/clients/queryKeys";
import { agentsKeys } from "@/features/servers/queryKeys";
import { usersKeys } from "@/features/users/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

// Pull the first failing target's result_text as the job's failure reason.
// The jobs list endpoint doesn't carry a top-level error field yet — the
// actionable signal lives on JobTarget.result_text for targets whose status
// is failed/expired. Concatenating every failed target would blow up the
// table cell; one is enough for operators to spot the cause.
function pickFailureReason(j: Job): string | undefined {
  if (j.status !== "failed" && j.status !== "expired") return undefined;
  const firstFailed = j.targets?.find(
    (t) => t.status === "failed" || t.status === "expired",
  );
  const text = firstFailed?.result_text?.trim();
  if (text) return text;
  // No per-target failure yet — the job itself must have expired before any
  // target reported. Surface that as the reason so "failed" isn't a dead end.
  if (j.status === "expired") return "Job expired before any agent reported back.";
  return undefined;
}

// Action slugs like "client.update" or "node.enroll" embed the entity kind in
// their namespace prefix. Map the few namespaces we care about to a target
// kind so the UI can pick the right lookup table.
function targetKindFromAction(action: string): string | undefined {
  const i = action.indexOf(".");
  if (i < 0) return undefined;
  const ns = action.slice(0, i);
  switch (ns) {
    case "user":
    case "auth":
    case "session":
      return "user";
    case "client":
      return "client";
    case "agent":
    case "node":
    case "enrollment":
    case "fleet":
      return "agent";
    default:
      return undefined;
  }
}

export function useActivity() {
  const { profile } = useProfile();
  const isAdmin = profile?.role === "admin";

  // M-8: when the WebSocket is healthy, every mutation already arrives
  // via the live event channel — refetching jobs/audit on a 15s timer
  // is redundant churn. Drop the cadence to a slow keep-alive (60s)
  // when WS is open, fall back to the original 15s refresh while WS
  // is connecting/reconnecting/closed so the page does not silently
  // freeze if the live feed is down.
  const refetchInterval = useEventAwareInterval(60_000, 15_000);

  const jobsQuery = useQuery({
    queryKey: ["jobs"],
    queryFn: () => apiClient.jobs(),
    refetchInterval,
  });

  const auditQuery = useQuery({
    queryKey: ["audit"],
    queryFn: () => apiClient.audit(),
    refetchInterval,
  });

  // Lookup tables. /api/users is admin-only — skip the fetch for non-admins to
  // avoid a 403 in the query error channel. Agents + clients are open to any
  // authenticated user, so their label maps always populate.
  // BP-02: cross-feature lookups read keys from the owning feature's
  // factory so the cache identity stays canonical (no string-literal
  // drift between activity and the source feature's hooks).
  const usersQuery = useQuery({
    queryKey: usersKeys.list(),
    queryFn: () => apiClient.users(),
    enabled: isAdmin,
    staleTime: 60_000,
  });

  const agentsQuery = useQuery({
    queryKey: agentsKeys.list(),
    queryFn: () => apiClient.agents(),
    staleTime: 30_000,
  });

  const clientsQuery = useQuery({
    queryKey: clientsKeys.list(),
    queryFn: () => apiClient.clients(),
    staleTime: 30_000,
  });

  const userById = new Map<string, string>();
  for (const u of usersQuery.data ?? []) userById.set(u.id, u.username);

  const agentById = new Map<string, string>();
  for (const a of agentsQuery.data ?? []) agentById.set(a.id, a.node_name);

  const clientById = new Map<string, string>();
  for (const c of clientsQuery.data ?? []) clientById.set(c.id, c.name);

  function resolveActor(id: string): string | undefined {
    return userById.get(id);
  }

  function resolveTarget(id: string, kind: string | undefined): string | undefined {
    if (!id) return undefined;
    if (kind === "user") return userById.get(id);
    if (kind === "agent") return agentById.get(id);
    if (kind === "client") return clientById.get(id);
    // No hint from the namespace — try each in turn. Cheap because the maps
    // are all O(1) lookups.
    return userById.get(id) ?? agentById.get(id) ?? clientById.get(id);
  }

  const jobs: JobListItem[] = (jobsQuery.data ?? []).map((j) => ({
    id: j.id,
    action: j.action,
    status: j.status,
    actorId: j.actor_id,
    actorLabel: resolveActor(j.actor_id),
    targetCount: j.target_agent_ids?.length ?? 0,
    createdAtUnix: Math.floor(new Date(j.created_at).getTime() / 1000),
    failureReason: pickFailureReason(j),
  }));

  const auditEvents: AuditListItem[] = (auditQuery.data ?? []).map((e) => {
    const kind = targetKindFromAction(e.action);
    return {
      id: e.id,
      actorId: e.actor_id,
      actorLabel: resolveActor(e.actor_id),
      action: e.action,
      targetId: e.target_id,
      targetLabel: resolveTarget(e.target_id, kind),
      targetKind: kind,
      createdAtUnix: Math.floor(new Date(e.created_at).getTime() / 1000),
    };
  });

  // Lookup failures don't block the page — without labels, actor/target
  // cells render as UUIDs. But silently swallowing the error leaves the
  // operator thinking the UUIDs are expected, when actually /api/agents
  // or /api/clients is 5xx-ing. Surface it as a non-fatal warning so the
  // container can render a banner without hiding the list.
  const lookupError = (() => {
    const src =
      (isAdmin && usersQuery.error) ||
      agentsQuery.error ||
      clientsQuery.error ||
      null;
    if (!src) return null;
    const msg = src instanceof Error ? src.message : String(src);
    return `Actor and target labels unavailable — lookup failed: ${msg}`;
  })();

  return {
    jobs,
    auditEvents,
    isLoading: jobsQuery.isLoading || auditQuery.isLoading,
    error: jobsQuery.error ?? auditQuery.error,
    lookupError,
  };
}
