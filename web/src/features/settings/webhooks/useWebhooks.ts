import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/shared/api/api";
import type {
  CreateWebhookEndpointInput,
  UpdateWebhookEndpointInput,
  WebhookEndpoint,
} from "@/shared/api/webhooks";

import { webhooksKeys } from "./queryKeys";

export function useWebhooks() {
  const qc = useQueryClient();

  const query = useQuery({
    queryKey: webhooksKeys.list(),
    queryFn: () => apiClient.webhookEndpoints(),
  });

  const endpoints: WebhookEndpoint[] = query.data?.endpoints ?? [];

  const createWebhook = useMutation({
    mutationFn: (payload: CreateWebhookEndpointInput) =>
      apiClient.createWebhookEndpoint(payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: webhooksKeys.all }),
  });

  const updateWebhook = useMutation({
    mutationFn: ({
      id,
      payload,
    }: {
      id: string;
      payload: UpdateWebhookEndpointInput;
    }) => apiClient.updateWebhookEndpoint(id, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: webhooksKeys.all }),
  });

  const deleteWebhook = useMutation({
    mutationFn: (id: string) => apiClient.deleteWebhookEndpoint(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: webhooksKeys.all }),
  });

  return {
    endpoints,
    isLoading: query.isLoading,
    error: query.error,
    createWebhook,
    updateWebhook,
    deleteWebhook,
  };
}
