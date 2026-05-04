import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ClientFormData } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import type { ClientInput } from "@/shared/api/api";
import { clientsKeys } from "@/features/clients/queryKeys";

function formToInput(form: ClientFormData): ClientInput {
  return {
    name: form.name,
    user_ad_tag: form.userAdTag,
    user_ad_tag_auto: form.userAdTagAuto,
    max_tcp_conns: form.maxTcpConns,
    max_unique_ips: form.maxUniqueIps,
    data_quota_bytes: form.dataQuotaBytes,
    expiration_rfc3339: form.expirationRfc3339,
    fleet_group_ids: form.fleetGroupIds,
    agent_ids: form.agentIds,
  };
}

export function useClientCreate() {
  const qc = useQueryClient();

  const mutation = useMutation({
    mutationFn: (data: ClientFormData) => apiClient.createClient(formToInput(data)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: clientsKeys.all });
    },
  });

  return mutation;
}
