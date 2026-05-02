import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { UserListItem } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

function transformUsers(raw: Awaited<ReturnType<typeof apiClient.users>>): UserListItem[] {
  return raw.map((u) => ({
    id: u.id,
    username: u.username,
    role: u.role as UserListItem["role"],
    totpEnabled: u.totp_enabled,
    createdAt: u.created_at ?? "",
  }));
}

export function useUsers() {
  const qc = useQueryClient();
  const refetchInterval = useEventAwareInterval(90_000, 30_000);

  const query = useQuery({
    queryKey: ["users"],
    queryFn: () => apiClient.users(),
    refetchInterval,
  });

  const users: UserListItem[] = query.data ? transformUsers(query.data) : [];

  const createUser = useMutation({
    mutationFn: (data: { username: string; password: string; role: string }) =>
      apiClient.createUser(data),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const updateUser = useMutation({
    mutationFn: ({ userId, data }: { userId: string; data: { username: string; role: string; new_password?: string } }) =>
      apiClient.updateUser(userId, data),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const deleteUser = useMutation({
    mutationFn: (userId: string) => apiClient.deleteUser(userId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const resetTotp = useMutation({
    mutationFn: (userId: string) => apiClient.resetUserTotp(userId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  return { users, isLoading: query.isLoading, createUser, updateUser, deleteUser, resetTotp };
}
