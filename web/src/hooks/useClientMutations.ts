import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ClientFormData } from "@lost-coder/panvex-ui";
import type { Client as ApiClient } from "@/lib/api";
import { apiClient } from "@/lib/api";
import { buildClientInput } from "@/lib/transforms/clients";
import { useToast } from "@/providers/ToastProvider";

// P2-FE-03: every mutation here surfaces its failure through the global
// toast channel. Before this, onError was unhandled and React-Query would
// log to the console while the UI sat silently — operators had no clue
// why a save button appeared to "do nothing". We still let callers chain
// their own onError for screen-specific side effects (e.g. closing a
// sheet); this hook's onError only handles the user-facing notification.
export function useClientMutations(clientId: string, rawClient: ApiClient | undefined) {
  const qc = useQueryClient();
  const toast = useToast();

  const editMutation = useMutation({
    mutationFn: (data: ClientFormData) => {
      if (!rawClient) throw new Error("Client data not loaded");
      return apiClient.updateClient(clientId, buildClientInput(data, rawClient));
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["client", clientId] });
      qc.invalidateQueries({ queryKey: ["clients"] });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  const rotateMutation = useMutation({
    mutationFn: () => apiClient.rotateClientSecret(clientId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["client", clientId] });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => apiClient.deleteClient(clientId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["clients"] });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  return { editMutation, rotateMutation, deleteMutation };
}
