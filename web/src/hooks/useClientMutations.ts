import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ClientFormData } from "@panvex/ui";
import type { Client as ApiClient } from "@/lib/api";
import { apiClient } from "@/lib/api";
import { buildClientInput } from "@/lib/transforms/clients";

export function useClientMutations(clientId: string, rawClient: ApiClient | undefined) {
  const qc = useQueryClient();

  const editMutation = useMutation({
    mutationFn: (data: ClientFormData) => {
      if (!rawClient) throw new Error("Client data not loaded");
      return apiClient.updateClient(clientId, buildClientInput(data, rawClient));
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["client", clientId] });
      qc.invalidateQueries({ queryKey: ["clients"] });
    },
  });

  const rotateMutation = useMutation({
    mutationFn: () => apiClient.rotateClientSecret(clientId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["client", clientId] });
    },
  });

  return { editMutation, rotateMutation };
}
