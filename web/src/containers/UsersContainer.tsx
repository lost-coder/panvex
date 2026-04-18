import { useState } from "react";
import { Spinner, type UserFormData, type UserFormSheetProps } from "@lost-coder/panvex-ui";
import { UsersManagementPage } from "@lost-coder/panvex-ui/pages";
import { useUsers } from "@/hooks/useUsers";
import { useConfirm } from "@/providers/ConfirmProvider";

type SheetState =
  | { mode: "closed" }
  | { mode: "create" }
  | { mode: "edit"; userId: string };

const emptyForm: UserFormData = { username: "", password: "", role: "viewer" };

export function UsersContainer() {
  const { users, isLoading, createUser, updateUser, deleteUser, resetTotp } = useUsers();
  const confirm = useConfirm();
  const [sheet, setSheet] = useState<SheetState>({ mode: "closed" });
  const [formData, setFormData] = useState<UserFormData>(emptyForm);
  const [formError, setFormError] = useState("");

  // P2-UX-04: deleting a user revokes access immediately. Confirm with
  // the username so the operator can't mistake which row they clicked.
  const handleDelete = async (userId: string) => {
    const user = users.find((u) => u.id === userId);
    const name = user?.username ?? "this user";
    const ok = await confirm({
      title: "Delete user?",
      body: `"${name}" will be removed from the control-plane and their sessions will end immediately.`,
      confirmLabel: "Delete user",
      variant: "danger",
    });
    if (!ok) return;
    deleteUser.mutate(userId);
  };

  // P2-UX-04: TOTP reset forces the user back to QR enrollment on next
  // login. Mildly disruptive — confirm with the username.
  const handleResetTotp = async (userId: string) => {
    const user = users.find((u) => u.id === userId);
    const name = user?.username ?? "this user";
    const ok = await confirm({
      title: "Reset TOTP?",
      body: `"${name}" will need to re-enroll their authenticator on next login.`,
      confirmLabel: "Reset TOTP",
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
          onError: (err) => setFormError(err instanceof Error ? err.message : "Failed to create user"),
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
          onError: (err) => setFormError(err instanceof Error ? err.message : "Failed to update user"),
        },
      );
    }
  };

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  const sheetProps: UserFormSheetProps | undefined = sheet.mode !== "closed"
    ? {
        mode: sheet.mode,
        data: formData,
        onChange: setFormData,
        onSubmit: handleSubmit,
        onCancel: () => setSheet({ mode: "closed" }),
        loading: createUser.isPending || updateUser.isPending,
        error: formError,
      }
    : undefined;

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
