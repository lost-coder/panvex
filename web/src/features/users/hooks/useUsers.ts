import { useMemo } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { UserListItem } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { usersKeys } from "@/features/users/queryKeys";
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
    queryKey: usersKeys.list(),
    queryFn: () => apiClient.users(),
    refetchInterval,
  });

  // Q3.U-P-06: memoise derivations on query.data identity (#web-2).
  const users: UserListItem[] = useMemo(
    () => (query.data ? transformUsers(query.data) : []),
    [query.data],
  );

  const createUser = useMutation({
    mutationFn: (data: { username: string; password: string; role: string }) =>
      apiClient.createUser(data),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKeys.all }),
  });

  const updateUser = useMutation({
    mutationFn: ({ userId, data }: { userId: string; data: { username: string; role: string; new_password?: string } }) =>
      apiClient.updateUser(userId, data),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKeys.all }),
  });

  const deleteUser = useMutation({
    mutationFn: (userId: string) => apiClient.deleteUser(userId),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKeys.all }),
  });

  const resetTotp = useMutation({
    mutationFn: (userId: string) => apiClient.resetUserTotp(userId),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKeys.all }),
  });

  return { users, isLoading: query.isLoading, error: query.error, refetch: query.refetch, createUser, updateUser, deleteUser, resetTotp };
}
