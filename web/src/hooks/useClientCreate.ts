import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ClientFormData } from "@lost-coder/panvex-ui";
import { apiClient } from "@/lib/api";
import type { ClientInput } from "@/lib/api";

function formToInput(form: ClientFormData): ClientInput {
  return {
    name: form.name,
    user_ad_tag: form.userAdTag,
    max_tcp_conns: form.maxTcpConns,
    max_unique_ips: form.maxUniqueIps,
    data_quota_bytes: form.dataQuotaBytes,
    expiration_rfc3339: form.expirationRfc3339,
    fleet_group_ids: [],
    agent_ids: [],
  };
}

export function useClientCreate() {
  const qc = useQueryClient();

  const mutation = useMutation({
    mutationFn: (data: ClientFormData) => apiClient.createClient(formToInput(data)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["clients"] });
    },
  });

  return mutation;
}
