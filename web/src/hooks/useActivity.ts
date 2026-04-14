import { useQuery } from "@tanstack/react-query";
import type { JobListItem, AuditListItem } from "@lost-coder/panvex-ui";
import { apiClient } from "@/lib/api";

function transformJobs(raw: Awaited<ReturnType<typeof apiClient.jobs>>): JobListItem[] {
  return raw.map((j) => ({
    id: j.id,
    action: j.action,
    status: j.status,
    actorId: j.actor_id,
    targetCount: j.target_agent_ids?.length ?? 0,
    createdAtUnix: Math.floor(new Date(j.created_at).getTime() / 1000),
  }));
}

function transformAudit(raw: Awaited<ReturnType<typeof apiClient.audit>>): AuditListItem[] {
  return raw.map((e) => ({
    id: e.id,
    actorId: e.actor_id,
    action: e.action,
    targetId: e.target_id,
    createdAtUnix: Math.floor(new Date(e.created_at).getTime() / 1000),
  }));
}

export function useActivity() {
  const jobsQuery = useQuery({
    queryKey: ["jobs"],
    queryFn: () => apiClient.jobs(),
    refetchInterval: 15_000,
  });

  const auditQuery = useQuery({
    queryKey: ["audit"],
    queryFn: () => apiClient.audit(),
    refetchInterval: 15_000,
  });

  const jobs: JobListItem[] = jobsQuery.data ? transformJobs(jobsQuery.data) : [];
  const auditEvents: AuditListItem[] = auditQuery.data ? transformAudit(auditQuery.data) : [];

  return {
    jobs,
    auditEvents,
    isLoading: jobsQuery.isLoading || auditQuery.isLoading,
    error: jobsQuery.error ?? auditQuery.error,
  };
}
