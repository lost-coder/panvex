import { SectionHeader } from "@/ui/layout/SectionHeader";
import { Badge } from "@/ui/primitives/Badge";
import { Button } from "@/ui/base/button";
import { DataTable } from "@/ui/components/DataTable";
import { roleVariant } from "@/ui/lib/status";
import type { UsersSectionProps, UserListItem } from "@/shared/api/types-pages/pages";

export function UsersSection({ users, onAdd, onEdit, onResetTotp, onDelete }: UsersSectionProps) {
  const columns = [
    {
      key: "username",
      header: "Username",
      render: (u: UserListItem) => (
        <span className="text-sm font-medium text-fg">{u.username}</span>
      ),
    },
    {
      key: "role",
      header: "Role",
      render: (u: UserListItem) => (
        <Badge variant={roleVariant[u.role] ?? "default"}>{u.role}</Badge>
      ),
    },
    {
      key: "totp",
      header: "2FA",
      render: (u: UserListItem) => (
        <span className={`text-xs ${u.totpEnabled ? "text-status-ok" : "text-fg-muted"}`}>
          {u.totpEnabled ? "Enabled" : "Off"}
        </span>
      ),
    },
    {
      key: "actions",
      header: "",
      render: (u: UserListItem) => (
        <div className="flex gap-1 justify-end">
          <Button variant="ghost" size="sm" onClick={() => onEdit(u.id)}>
            Edit
          </Button>
          {u.totpEnabled && (
            <Button variant="ghost" size="sm" onClick={() => onResetTotp(u.id)}>
              Reset 2FA
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onDelete(u.id)}
            className="text-status-error hover:text-status-error"
          >
            Delete
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <SectionHeader title="Panel Users" />
        <Button size="sm" onClick={onAdd}>
          Add User
        </Button>
      </div>
      <DataTable data={users} columns={columns} keyExtractor={(u) => u.id} />
    </div>
  );
}
