// --- User Management ---

export interface UserListItem {
  id: string;
  username: string;
  role: "admin" | "operator" | "viewer";
  totpEnabled: boolean;
  createdAt: string;
}

export interface UserFormData {
  username: string;
  password: string;
  role: "admin" | "operator" | "viewer";
}

export interface UserFormSheetProps {
  mode: "create" | "edit";
  data: UserFormData;
  onChange: (data: UserFormData) => void;
  onSubmit: () => void;
  onCancel: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
}

export interface UsersManagementPageProps {
  users: UserListItem[];
  onAdd: () => void;
  onEdit: (userId: string) => void;
  onDelete: (userId: string) => void;
  onResetTotp: (userId: string) => void;
  sheet?: UserFormSheetProps | undefined;
}
