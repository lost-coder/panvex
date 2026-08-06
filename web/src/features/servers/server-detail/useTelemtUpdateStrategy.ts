// Task 13: TanStack Query hooks for the per-agent Telemt update strategy
// (GET/PUT /api/agents/{id}/telemt/update-strategy, Task 9). Mirrors the
// config-feature hooks convention (see servers/config/configHooks.ts):
// the read side is a plain useQuery, the write side toasts on failure and
// invalidates the read query on success — the caller decides whether to
// also show a success toast (see TelemtUpdateSection's handleSave).

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { ApiError, apiClient, type TelemtUpdateStrategy } from "@/shared/api/api";
import { telemtUpdateKeys } from "@/features/servers/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";

// Task 14: 409 guard codes the dispatch endpoint returns (see
// handleDispatchTelemtUpdate's doc comment in
// internal/controlplane/server/http_telemt_update.go) that have a
// dedicated localized toast. Anything else (network error, 500, an
// unrecognised future code) falls back to the raw error message.
const DISPATCH_ERROR_CODES = [
  "strategy_not_configured",
  "mode_not_binary",
  "update_unavailable",
  "no_known_release",
] as const;
type DispatchErrorCode = (typeof DISPATCH_ERROR_CODES)[number];

function isDispatchErrorCode(code: string | undefined): code is DispatchErrorCode {
  return !!code && (DISPATCH_ERROR_CODES as readonly string[]).includes(code);
}

export function useTelemtUpdateStrategy(agentId: string) {
  return useQuery({
    queryKey: telemtUpdateKeys.strategy(agentId),
    queryFn: ({ signal }) => apiClient.getTelemtUpdateStrategy(agentId, { signal }),
    enabled: !!agentId,
  });
}

export function usePutTelemtUpdateStrategy(agentId: string) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: (strategy: TelemtUpdateStrategy) =>
      apiClient.putTelemtUpdateStrategy(agentId, strategy),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: telemtUpdateKeys.strategy(agentId) });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });
}

// Task 14: dispatch an in-place Telemt update. Success just toasts —
// the new job shows up in the existing jobs feed via its own WS-driven
// invalidation (event-invalidations.ts already refetches ["jobs"] on any
// "jobs.*" event), so no manual query invalidation is needed here.
// Failure maps the 409 guard `code` to a localized message; anything else
// (network error, 500, an unrecognised future code) falls back to the raw
// server-provided message.
export function useDispatchTelemtUpdate(agentId: string) {
  const toast = useToast();
  const { t } = useTranslation("servers");
  return useMutation({
    mutationFn: (version: string) => apiClient.dispatchTelemtUpdate(agentId, { version }),
    onSuccess: () => {
      toast.success(t("telemtUpdate.action.queued"));
    },
    onError: (err: Error) => {
      const code = err instanceof ApiError ? err.code : undefined;
      if (isDispatchErrorCode(code)) {
        toast.error(t(`telemtUpdate.action.error.${code}`));
        return;
      }
      toast.error(err.message);
    },
  });
}
