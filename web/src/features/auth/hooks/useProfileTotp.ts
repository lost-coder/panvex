import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { authKeys } from "@/features/auth/queryKeys";

export function useProfileTotp() {
  const qc = useQueryClient();

  const invalidateProfile = () => {
    void qc.invalidateQueries({ queryKey: authKeys.me() });
  };

  // No onError funnel here on purpose: the TOTP sheets render the failure
  // inline from mutation.error, and a toast on top of that would double-report.
  const setupMutation = useMutation({
    mutationFn: () => apiClient.startTotpSetup(),
  });

  const enableMutation = useMutation({
    mutationFn: (payload: { password: string; totp_code: string }) =>
      apiClient.enableTotp(payload),
    onSuccess: () => {
      invalidateProfile();
      // Clear TOTP secret from mutation cache after successful enable
      setupMutation.reset();
    },
  });

  const disableMutation = useMutation({
    mutationFn: (payload: { password: string; totp_code: string }) =>
      apiClient.disableTotp(payload),
    onSuccess: invalidateProfile,
  });

  return { setupMutation, enableMutation, disableMutation };
}
