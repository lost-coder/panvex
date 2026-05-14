// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: pulse row + role chips + denser table.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Badge,
  Button,
  DataTable,
  EmptyState,
  FilterBar,
  FilterChip,
  PageHeader,
  PulseRow,
  StatusLabel,
  cn,
  roleVariant,
} from "@/ui";
import { UserFormSheet } from "@/features/users/ui/UserFormSheet";
import type {
  UsersManagementPageProps,
  UserListItem,
} from "@/shared/api/types-pages/pages";

type RoleFilter = "all" | "admin" | "operator" | "viewer";

// Avatar — first letter of the username in a mono circle. Tone splits on
// role so a scroll-glance shows where admins cluster.
function AvatarInitial({ user }: Readonly<{ user: UserListItem }>) {
  const roleBg: Record<UserListItem["role"], string> = {
    admin: "bg-accent/15 text-accent",
    operator: "bg-status-warn/15 text-status-warn",
    viewer: "bg-fg-faint/30 text-fg-muted",
  };
  return (
    <span
      className={cn(
        "inline-flex items-center justify-center h-7 w-7 rounded-full font-mono text-xs font-semibold uppercase shrink-0",
        roleBg[user.role],
      )}
      aria-hidden
    >
      {user.username.slice(0, 1)}
    </span>
  );
}

function useFormatRelative() {
  const { t } = useTranslation("users");
  return (iso: string | undefined) => {
    if (!iso) return "—";
    const ts = Date.parse(iso);
    if (!Number.isFinite(ts)) return "—";
    const diff = Math.floor((Date.now() - ts) / 1000);
    if (diff < 60) return t("relative.justNow");
    if (diff < 3_600) return t("relative.minutesAgo", { count: Math.floor(diff / 60) });
    if (diff < 86_400) return t("relative.hoursAgo", { count: Math.floor(diff / 3_600) });
    if (diff < 30 * 86_400) return t("relative.daysAgo", { count: Math.floor(diff / 86_400) });
    const d = new Date(ts);
    return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  };
}

type TickTone = "default" | "ok" | "warn" | "error";

function totpTone(total: number, pct: number): TickTone {
  if (total === 0) return "default";
  if (pct === 100) return "ok";
  if (pct >= 50) return "warn";
  return "error";
}

// ─── Main ─────────────────────────────────────────────────────────────────────

export function UsersManagementPage({
  users,
  onAdd,
  onEdit,
  onDelete,
  onResetTotp,
  sheet,
}: Readonly<UsersManagementPageProps>) {
  const { t } = useTranslation("users");
  const formatRelative = useFormatRelative();
  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState<RoleFilter>("all");

  const counts = useMemo(() => {
    const byRole = { admin: 0, operator: 0, viewer: 0 };
    let totp = 0;
    for (const u of users) {
      byRole[u.role]++;
      if (u.totpEnabled) totp++;
    }
    return { total: users.length, ...byRole, totp };
  }, [users]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return users.filter((u) => {
      if (roleFilter !== "all" && u.role !== roleFilter) return false;
      if (!q) return true;
      return u.username.toLowerCase().includes(q) || u.role.toLowerCase().includes(q);
    });
  }, [users, query, roleFilter]);

  const columns = [
    {
      key: "user",
      header: t("table.user"),
      render: (u: Readonly<UserListItem>) => (
        <div className="flex items-center gap-3 min-w-0">
          <AvatarInitial user={u} />
          <div className="flex flex-col min-w-0">
            <span className="text-sm font-medium text-fg truncate">{u.username}</span>
            <span className="text-[10px] font-mono text-fg-muted">
              {t("table.addedRelative", { when: formatRelative(u.createdAt) })}
            </span>
          </div>
        </div>
      ),
      className: "min-w-[220px]",
    },
    {
      key: "role",
      header: t("table.role"),
      render: (u: Readonly<UserListItem>) => (
        <Badge variant={roleVariant[u.role] ?? "default"}>{u.role}</Badge>
      ),
      className: "w-[120px]",
    },
    {
      key: "totp",
      header: t("table.totp"),
      render: (u: Readonly<UserListItem>) => (
        <StatusLabel
          tone={u.totpEnabled ? "ok" : "warn"}
          label={u.totpEnabled ? t("totp.enabled") : t("totp.disabled")}
        />
      ),
      className: "w-[110px]",
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
      className: "text-right",
    },
  ];

  const totpPct =
    counts.total === 0 ? 0 : Math.round((counts.totp / counts.total) * 100);

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("page.title")}
        subtitle={t("page.subtitle", { count: users.length })}
        trailing={
          <Button size="sm" onClick={onAdd}>
            {t("page.add")}
          </Button>
        }
      />

      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        <PulseRow
          ticks={[
            {
              label: t("pulse.total"),
              value: counts.total.toLocaleString(),
              hint: t("pulse.totalHint", { count: counts.viewer }),
            },
            {
              label: t("pulse.admins"),
              value: counts.admin.toLocaleString(),
              hint: counts.admin === 0 ? t("pulse.adminsNone") : t("pulse.adminsFullAccess"),
              tone: counts.admin === 0 ? "error" : "default",
            },
            {
              label: t("pulse.operators"),
              value: counts.operator.toLocaleString(),
              hint: t("pulse.operatorsHint"),
            },
            {
              label: t("pulse.totpCoverage"),
              value: `${totpPct}%`,
              hint: t("pulse.totpHint", { enrolled: counts.totp, total: counts.total }),
              tone: totpTone(counts.total, totpPct),
            },
          ]}
        />

        {/* Filter row: role chips + search */}
        <FilterBar
          chips={
            <>
              <FilterChip
                active={roleFilter === "all"}
                onClick={() => setRoleFilter("all")}
                count={counts.total}
              >
                {t("filter.all")}
              </FilterChip>
              <FilterChip
                active={roleFilter === "admin"}
                onClick={() => setRoleFilter("admin")}
                count={counts.admin}
              >
                {t("filter.admin")}
              </FilterChip>
              <FilterChip
                active={roleFilter === "operator"}
                onClick={() => setRoleFilter("operator")}
                count={counts.operator}
              >
                {t("filter.operator")}
              </FilterChip>
              <FilterChip
                active={roleFilter === "viewer"}
                onClick={() => setRoleFilter("viewer")}
                count={counts.viewer}
              >
                {t("filter.viewer")}
              </FilterChip>
            </>
          }
          search={{
            value: query,
            onChange: setQuery,
            placeholder: t("filter.searchPlaceholder"),
          }}
        />

        {filtered.length === 0 ? (
          <EmptyState
            title={users.length === 0 ? t("empty.noUsersTitle") : t("empty.noMatchTitle")}
            description={
              users.length === 0
                ? t("empty.noUsersDescription")
                : t("empty.noMatchDescription")
            }
          />
        ) : (
          <DataTable data={filtered} columns={columns} keyExtractor={(u) => u.id} />
        )}
      </div>

      {sheet && (
        <UserFormSheet
          mode={sheet.mode}
          data={sheet.data}
          onChange={sheet.onChange}
          onSubmit={sheet.onSubmit}
          onCancel={sheet.onCancel}
          loading={sheet.loading}
          error={sheet.error}
        />
      )}
    </div>
  );
}
