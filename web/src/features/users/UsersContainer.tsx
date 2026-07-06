import { useState } from "react";
import { useTranslation } from "react-i18next";
import { type UserFormData, type UserFormSheetProps } from "@/ui";
import { SkeletonRows } from "@/ui";
import { UsersManagementPage } from "./UsersManagementPage";
import { ErrorState } from "@/components/ErrorState";
import { useUsers } from "./hooks/useUsers";
import { useConfirm } from "@/app/providers/ConfirmProvider";

type SheetState =
  | { mode: "closed" }
  | { mode: "create" }
  | { mode: "edit"; userId: string };

const emptyForm: UserFormData = { username: "", password: "", role: "viewer" };

export function UsersContainer() {
  const { t } = useTranslation("users");
  const { users, isLoading, error, refetch, createUser, updateUser, deleteUser, resetTotp } = useUsers();
  const confirm = useConfirm();
  const [sheet, setSheet] = useState<SheetState>({ mode: "closed" });
  const [formData, setFormData] = useState<UserFormData>(emptyForm);
  const [formError, setFormError] = useState("");

  // P2-UX-04: deleting a user revokes access immediately. Confirm with
  // the username so the operator can't mistake which row they clicked.
  const handleDelete = async (userId: string) => {
    const user = users.find((u) => u.id === userId);
    const name = user?.username ?? t("confirm.fallbackName");
    const ok = await confirm({
      title: t("confirm.deleteTitle"),
      body: t("confirm.deleteBody", { name }),
      confirmLabel: t("confirm.deleteConfirm"),
      variant: "danger",
      // UX-05: deletion revokes all sessions + audit continuity — gate on
      // typing the exact username so a misclick on an admin row cannot
      // lock everyone out.
      requireTypeMatch: user?.username,
    });
    if (!ok) return;
    deleteUser.mutate(userId);
  };

  // P2-UX-04: TOTP reset forces the user back to QR enrollment on next
  // login. Mildly disruptive — confirm with the username.
  const handleResetTotp = async (userId: string) => {
    const user = users.find((u) => u.id === userId);
    const name = user?.username ?? t("confirm.fallbackName");
    const ok = await confirm({
      title: t("confirm.resetTotpTitle"),
      body: t("confirm.resetTotpBody", { name }),
      confirmLabel: t("confirm.resetTotpConfirm"),
    });
    if (!ok) return;
    resetTotp.mutate(userId);
  };

  const handleAdd = () => {
    setFormData(emptyForm);
    setFormError("");
    setSheet({ mode: "create" });
  };

  const handleEdit = (userId: string) => {
    const user = users.find((u) => u.id === userId);
    if (!user) return;
    setFormData({ username: user.username, password: "", role: user.role });
    setFormError("");
    setSheet({ mode: "edit", userId });
  };

  const handleSubmit = () => {
    setFormError("");
    if (sheet.mode === "create") {
      createUser.mutate(
        { username: formData.username, password: formData.password, role: formData.role },
        {
          onSuccess: () => setSheet({ mode: "closed" }),
          onError: (err) => setFormError(err instanceof Error ? err.message : t("form.errorCreate")),
        },
      );
    } else if (sheet.mode === "edit") {
      const payload: { username: string; role: string; new_password?: string } = {
        username: formData.username,
        role: formData.role,
      };
      if (formData.password) payload.new_password = formData.password;
      updateUser.mutate(
        { userId: sheet.userId, data: payload },
        {
          onSuccess: () => setSheet({ mode: "closed" }),
          onError: (err) => setFormError(err instanceof Error ? err.message : t("form.errorUpdate")),
        },
      );
    }
  };

  if (isLoading) {
    // 6.1: skeleton rows mirror the list layout so the page doesn't
    // jump when data arrives, and screen readers announce "Загрузка
    // списка…" once (see SkeletonRows contract).
    return (
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={6} />
      </div>
    );
  }

  if (error) {
    return (
      <ErrorState
        title={t("error.loadUsers")}
        description={error.message || t("error.fallbackDescription")}
        onRetry={() => void refetch()}
      />
    );
  }

  const sheetProps: UserFormSheetProps | undefined = sheet.mode === "closed"
    ? undefined
    : {
        mode: sheet.mode,
        data: formData,
        onChange: setFormData,
        onSubmit: handleSubmit,
        onCancel: () => setSheet({ mode: "closed" }),
        loading: createUser.isPending || updateUser.isPending,
        error: formError,
      };

  return (
    <UsersManagementPage
      users={users}
      onAdd={handleAdd}
      onEdit={handleEdit}
      onDelete={handleDelete}
      onResetTotp={handleResetTotp}
      sheet={sheetProps}
    />
  );
}
