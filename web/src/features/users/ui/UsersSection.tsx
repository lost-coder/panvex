import { useTranslation } from "react-i18next";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { Badge } from "@/ui/primitives/Badge";
import { Button } from "@/ui/base/button";
import { DataTable } from "@/ui/components/DataTable";
import { roleVariant } from "@/ui/lib/status";
import type { UsersSectionProps, UserListItem } from "@/shared/api/types-pages/pages";

export function UsersSection({ users, onAdd, onEdit, onResetTotp, onDelete }: Readonly<UsersSectionProps>) {
  const { t } = useTranslation("users");
  const columns = [
    {
      key: "username",
      header: t("table.username"),
      render: (u: Readonly<UserListItem>) => (
        <span className="text-sm font-medium text-fg">{u.username}</span>
      ),
    },
    {
      key: "role",
      header: t("table.role"),
      render: (u: Readonly<UserListItem>) => (
        <Badge variant={roleVariant[u.role] ?? "default"}>{u.role}</Badge>
      ),
    },
    {
      key: "totp",
      header: t("table.totp"),
      render: (u: Readonly<UserListItem>) => (
        <span className={`text-xs ${u.totpEnabled ? "text-status-ok" : "text-fg-muted"}`}>
          {u.totpEnabled ? t("totp.enabled") : t("totp.disabled")}
        </span>
      ),
    },
    {
      key: "actions",
      header: "",
      render: (u: Readonly<UserListItem>) => (
        <div className="flex gap-1 justify-end">
          <Button variant="ghost" size="sm" onClick={() => onEdit(u.id)}>
            {t("actions.edit")}
          </Button>
          {u.totpEnabled && (
            <Button variant="ghost" size="sm" onClick={() => onResetTotp(u.id)}>
              {t("actions.resetTotp")}
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onDelete(u.id)}
            className="text-status-error hover:text-status-error"
          >
            {t("actions.delete")}
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <SectionHeader title={t("section.title")} />
        <Button size="sm" onClick={onAdd}>
          {t("page.add")}
        </Button>
      </div>
      <DataTable data={users} columns={columns} keyExtractor={(u) => u.id} />
    </div>
  );
}
