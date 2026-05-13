import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { apiClient } from "@/shared/api/api";
import type { EnrollmentAttempt } from "@/shared/api/types-enrollment";

import { EnrollmentTimeline } from "./EnrollmentTimeline";

interface Props {
  agentId: string;
}

function statusColor(status: EnrollmentAttempt["status"]): string {
  if (status === "success") return "text-emerald-600";
  if (status === "failed") return "text-red-600";
  return "text-amber-600";
}

// EnrollmentHistory renders the most recent enrollment attempts for an
// agent on the Server Detail page. Each row is collapsible; clicking
// fetches the full timeline lazily. The block hides itself when the
// agent has no attempts on record so older agents (enrolled before the
// recorder shipped) don't show a permanently empty section.
export function EnrollmentHistory({ agentId }: Props) {
  const list = useQuery({
    queryKey: ["enrollment-attempts", "by-agent", agentId],
    queryFn: () => apiClient.listEnrollmentAttempts({ agent_id: agentId, limit: 5 }),
  });

  const [expanded, setExpanded] = useState<string | null>(null);

  const detail = useQuery({
    queryKey: ["enrollment-attempts", "detail", expanded],
    queryFn: () => apiClient.getEnrollmentAttempt(expanded!),
    enabled: !!expanded,
  });

  if (list.isLoading) {
    return (
      <section className="mt-6">
        <div className="text-sm text-fg-muted">Загружаем историю…</div>
      </section>
    );
  }

  if (!list.data || list.data.items.length === 0) {
    return null;
  }

  return (
    <section className="mt-6">
      <h3 className="text-base font-medium mb-3">История подключений</h3>
      <ul className="flex flex-col gap-2">
        {list.data.items.map((a: EnrollmentAttempt) => {
          const isOpen = expanded === a.id;
          return (
            <li key={a.id} className="rounded-md border p-3">
              <button
                type="button"
                onClick={() => setExpanded(isOpen ? null : a.id)}
                className="flex w-full items-center justify-between text-left text-sm"
                aria-expanded={isOpen}
              >
                <span>
                  {new Date(a.started_at).toLocaleString()} · {a.mode}
                </span>
                <span className={statusColor(a.status)}>
                  {a.status}
                  {a.error_code ? ` (${a.error_code})` : ""}
                </span>
              </button>
              {isOpen && detail.data && (
                <div className="mt-3">
                  <EnrollmentTimeline detail={detail.data} />
                </div>
              )}
              {isOpen && detail.isLoading && (
                <div className="mt-3 text-xs text-fg-muted">Загружаем…</div>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
