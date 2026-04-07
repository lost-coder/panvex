import { useState } from "react";
import { UsersManagementPage, Spinner, type UserFormData, type UserFormSheetProps } from "@panvex/ui";
import { useUsers } from "@/hooks/useUsers";

type SheetState =
  | { mode: "closed" }
  | { mode: "create" }
  | { mode: "edit"; userId: string };

const emptyForm: UserFormData = { username: "", password: "", role: "viewer" };

export function UsersContainer() {
  const { users, isLoading, createUser, updateUser, deleteUser, resetTotp } = useUsers();
  const [sheet, setSheet] = useState<SheetState>({ mode: "closed" });
  const [formData, setFormData] = useState<UserFormData>(emptyForm);
  const [formError, setFormError] = useState("");

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
      onDelete={(userId) => deleteUser.mutate(userId)}
      onResetTotp={(userId) => resetTotp.mutate(userId)}
      sheet={sheetProps}
    />
  );
}
