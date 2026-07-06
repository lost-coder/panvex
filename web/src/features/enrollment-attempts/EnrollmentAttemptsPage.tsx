import { Fragment, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";

import { apiClient } from "@/shared/api/api";
import type {
  EnrollmentAttempt,
  EnrollmentAttemptsFilter,
  EnrollmentAttemptsPage as PageT,
  EnrollmentMode,
  EnrollmentStatus,
} from "@/shared/api/types-enrollment";
import { StatusLabel, formatDateTime, shortId, type StatusTone } from "@/ui";
import { EnrollmentTimeline } from "@/features/servers/enrollment/EnrollmentTimeline";

import { EnrollmentAttemptsFilters } from "./EnrollmentAttemptsFilters";
import { enrollmentAttemptsKeys } from "./queryKeys";

const PAGE_SIZE = 50;

function statusTone(s: EnrollmentAttempt["status"]): StatusTone {
  if (s === "success") return "ok";
  if (s === "failed") return "error";
  return "warn";
}

// readQueryFilters seeds the page filter from the URL — operators
// arriving via the Server Detail "View all" deep-link land with
// `?agent_id=…` already pinned. Unknown params are ignored. We don't
// keep the URL and React state in sync afterwards: the deep-link is
// one-shot, and round-tripping every filter into the address bar adds
// browser-history noise that operators flagged as annoying during the
// Phase-2 retro.
function readQueryFilters(): EnrollmentAttemptsFilter {
  if (typeof globalThis.window === "undefined") return {};
  const sp = new URLSearchParams(globalThis.location.search);
  const f: EnrollmentAttemptsFilter = {};
  const agentId = sp.get("agent_id");
  if (agentId) f.agent_id = agentId;
  const tokenId = sp.get("token_id");
  if (tokenId) f.token_id = tokenId;
  const status = sp.get("status");
  if (status) f.status = status as EnrollmentStatus;
  const mode = sp.get("mode");
  if (mode) f.mode = mode as EnrollmentMode;
  const errorCode = sp.get("error_code");
  if (errorCode) f.error_code = errorCode;
  const startedAfter = sp.get("started_after");
  if (startedAfter) f.started_after = startedAfter;
  const startedBefore = sp.get("started_before");
  if (startedBefore) f.started_before = startedBefore;
  return f;
}

// EnrollmentAttemptsPage is the fleet-wide enrollment observability
// page. The Server Detail history block (EnrollmentHistory) is an
// agent-scoped subset of the same data; this page lets operators slice
// by status / mode / error code / time window across the whole fleet.
//
// Pagination uses useInfiniteQuery rather than offset/limit URL
// shuffling: the backend hands us an opaque base64 cursor and we feed
// it back via fetchNextPage. The cursor stays inside React Query —
// it never lands in the browser URL because operators rarely want to
// deep-link to page N of an enrollment-attempts list.
export function EnrollmentAttemptsPage() {
  const { t } = useTranslation("enrollment-attempts");
  const [filter, setFilter] = useState<EnrollmentAttemptsFilter>(() =>
    readQueryFilters(),
  );

  const query = useInfiniteQuery({
    queryKey: enrollmentAttemptsKeys.page(filter),
    queryFn: ({ pageParam, signal }) =>
      apiClient.listEnrollmentAttempts(
        {
          ...filter,
          limit: PAGE_SIZE,
          cursor: pageParam as string | undefined,
        },
        { signal },
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: PageT) => last.next_cursor ?? undefined,
  });

  const items: EnrollmentAttempt[] = useMemo(
    () => query.data?.pages.flatMap((p) => p.items) ?? [],
    [query.data],
  );

  const [expanded, setExpanded] = useState<string | null>(null);
  const detail = useQuery({
    queryKey: enrollmentAttemptsKeys.detail(expanded!),
    queryFn: ({ signal }) => apiClient.getEnrollmentAttempt(expanded!, { signal }),
    enabled: !!expanded,
  });

  // U-10: resolve agent UUIDs to operator-facing node names. Raw UUIDs are
  // meaningless to a human and ate ~60% of the row width on mobile.
  const serversQuery = useQuery({
    queryKey: ["telemetry", "servers", "names"],
    queryFn: ({ signal }) => apiClient.telemetryServers({ signal }),
    staleTime: 60_000,
  });
  const nodeNames = useMemo(() => {
    const m = new Map<string, string>();
    for (const s of serversQuery.data?.servers ?? []) {
      if (s.agent?.id) m.set(s.agent.id, s.agent.node_name || s.agent.id);
    }
    return m;
  }, [serversQuery.data]);
  const nodeLabel = (agentId: string | undefined): string =>
    agentId ? (nodeNames.get(agentId) ?? shortId(agentId)) : "—";

  return (
    <div className="flex flex-col gap-4 p-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold text-fg">{t("page.title")}</h1>
        <p className="text-sm text-fg-muted">{t("page.subtitle")}</p>
      </header>

      <EnrollmentAttemptsFilters
        value={filter}
        onChange={setFilter}
        onReset={() => setFilter({})}
      />

      {items.length === 0 && !query.isLoading && (
        <div className="text-sm text-fg-muted">{t("empty")}</div>
      )}

      {items.length > 0 && (
        <>
          {/* Mobile (U-10): one card per attempt. Node name replaces the raw
              UUID; started-at uses the locale formatter; tap toggles the
              timeline. Avoids the cramped horizontal-scroll table on phones. */}
          <div className="md:hidden flex flex-col gap-2">
            {items.map((a) => {
              const isOpen = expanded === a.id;
              return (
                <div
                  key={a.id}
                  className="rounded-sm bg-bg-card border border-border overflow-hidden"
                >
                  <button
                    type="button"
                    onClick={() => setExpanded(isOpen ? null : a.id)}
                    className="w-full text-left px-3 py-2.5 flex flex-col gap-1"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium text-fg truncate">
                        {nodeLabel(a.agent_id)}
                      </span>
                      <StatusLabel
                        tone={statusTone(a.status)}
                        label={a.status}
                      />
                    </div>
                    <div className="flex items-center gap-2 text-micro font-mono text-fg-muted">
                      <span>{formatDateTime(a.started_at)}</span>
                      <span>·</span>
                      <span>{a.mode}</span>
                      {a.error_code && (
                        <span className="text-status-error">
                          · {a.error_code}
                        </span>
                      )}
                    </div>
                  </button>
                  {isOpen && detail.data && (
                    <div className="bg-bg-card-hi p-3 border-t border-divider">
                      <EnrollmentTimeline detail={detail.data} />
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          {/* Desktop table. */}
          <div className="hidden md:block overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-fg-muted">
                  <th className="py-1 font-normal whitespace-nowrap">
                    {t("table.startedAt")}
                  </th>
                  <th className="py-1 font-normal">{t("table.mode")}</th>
                  <th className="py-1 font-normal">{t("table.agent")}</th>
                  <th className="py-1 font-normal">{t("table.status")}</th>
                  <th className="py-1 font-normal">{t("table.errorCode")}</th>
                  <th className="py-1 font-normal">{t("table.requestId")}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((a) => {
                  const isOpen = expanded === a.id;
                  return (
                    <Fragment key={a.id}>
                      <tr
                        onClick={() => setExpanded(isOpen ? null : a.id)}
                        className="cursor-pointer border-t border-divider hover:bg-bg-card"
                      >
                        <td className="py-2 text-fg whitespace-nowrap">
                          {formatDateTime(a.started_at)}
                        </td>
                        <td className="text-fg-muted">{a.mode}</td>
                        <td className="text-fg" title={a.agent_id ?? undefined}>
                          {nodeLabel(a.agent_id)}
                        </td>
                        <td>
                          <StatusLabel
                            tone={statusTone(a.status)}
                            label={a.status}
                          />
                        </td>
                        <td className="text-fg-muted">{a.error_code ?? ""}</td>
                        <td className="font-mono text-xs text-fg-muted">
                          {a.request_id.slice(0, 8)}
                        </td>
                      </tr>
                      {isOpen && detail.data && (
                        <tr>
                          <td colSpan={6} className="bg-bg-card p-3">
                            <EnrollmentTimeline detail={detail.data} />
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        </>
      )}

      {query.hasNextPage && (
        <button
          type="button"
          onClick={() => query.fetchNextPage()}
          disabled={query.isFetchingNextPage}
          className="self-start rounded-md border border-divider px-3 py-1 text-sm text-fg-muted hover:text-fg disabled:opacity-50"
        >
          {t("loadMore")}
        </button>
      )}
    </div>
  );
}
