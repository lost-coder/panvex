import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { ClientFormData } from "@/shared/api/types-pages/pages";
import type { Client as ApiClient } from "@/shared/api/api";
import { apiClient } from "@/shared/api/api";
import { buildClientInput } from "@/shared/api/transforms/clients";
import { clientsKeys } from "@/features/clients/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";

// P2-FE-03: every mutation here surfaces its failure through the global
// toast channel. Before this, onError was unhandled and React-Query would
// log to the console while the UI sat silently — operators had no clue
// why a save button appeared to "do nothing". We still let callers chain
// their own onError for screen-specific side effects (e.g. closing a
// sheet); this hook's onError only handles the user-facing notification.
export function useClientMutations(clientId: string, rawClient: ApiClient | undefined) {
  const qc = useQueryClient();
  const toast = useToast();
  const { t } = useTranslation("clients");

  const editMutation = useMutation({
    mutationFn: (data: ClientFormData) => {
      if (!rawClient) throw new Error("Client data not loaded");
      return apiClient.updateClient(clientId, buildClientInput(data, rawClient));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: clientsKeys.detail(clientId) });
      void qc.invalidateQueries({ queryKey: clientsKeys.all });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  const rotateMutation = useMutation({
    mutationFn: () => apiClient.rotateClientSecret(clientId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: clientsKeys.detail(clientId) });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  // Re-runs the client.create rollout job. Needed when the initial
  // deployment on one or more agents failed (bad ad tag, unreachable
  // node, etc.) and the operator wants to retry without touching any
  // fields.
  const redeployMutation = useMutation({
    mutationFn: () => apiClient.redeployClient(clientId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: clientsKeys.detail(clientId) });
      void qc.invalidateQueries({ queryKey: clientsKeys.all });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => apiClient.deleteClient(clientId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: clientsKeys.all });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  /**
   * scheduleDeleteWithUndo defers the actual DELETE by 7 seconds and
   * surfaces an Undo button in the toast (2.6). Reverting is free —
   * nothing has happened yet — so no backend restore endpoint is
   * required. The real DELETE only fires when the undo window closes
   * without the user clicking Undo. Returns a canceller so containers
   * can also cancel programmatically (e.g. on unmount).
   */
  function scheduleDeleteWithUndo(displayName: string): () => void {
    let cancelled = false;
    const timer = globalThis.setTimeout(() => {
      if (cancelled) return;
      deleteMutation.mutate();
    }, 7000);

    const cancel = () => {
      cancelled = true;
      globalThis.clearTimeout(timer);
    };

    toast.withAction(
      "info",
      t("toasts.deleteScheduled", { name: displayName }),
      {
        label: t("toasts.undo"),
        onClick: () => {
          cancel();
          toast.info(t("toasts.deleteCancelled"));
        },
      },
      { duration: 7000 },
    );

    return cancel;
  }

  return { editMutation, rotateMutation, redeployMutation, deleteMutation, scheduleDeleteWithUndo };
}
