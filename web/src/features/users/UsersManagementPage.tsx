// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: pulse row + role chips + denser table.
import { useMemo, useState } from "react";
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

function formatRelative(iso: string | undefined) {
  if (!iso) return "—";
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "—";
  const diff = Math.floor((Date.now() - t) / 1000);
  if (diff < 60) return "just now";
  if (diff < 3_600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86_400) return `${Math.floor(diff / 3_600)}h ago`;
  if (diff < 30 * 86_400) return `${Math.floor(diff / 86_400)}d ago`;
  const d = new Date(t);
  return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
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
      header: "User",
      render: (u: Readonly<UserListItem>) => (
        <div className="flex items-center gap-3 min-w-0">
          <AvatarInitial user={u} />
          <div className="flex flex-col min-w-0">
            <span className="text-sm font-medium text-fg truncate">{u.username}</span>
            <span className="text-[10px] font-mono text-fg-muted">
              Added {formatRelative(u.createdAt)}
            </span>
          </div>
        </div>
      ),
      className: "min-w-[220px]",
    },
    {
      key: "role",
      header: "Role",
      render: (u: Readonly<UserListItem>) => (
        <Badge variant={roleVariant[u.role] ?? "default"}>{u.role}</Badge>
      ),
      className: "w-[120px]",
    },
    {
      key: "totp",
      header: "2FA",
      render: (u: Readonly<UserListItem>) => (
        <StatusLabel
          tone={u.totpEnabled ? "ok" : "warn"}
          label={u.totpEnabled ? "Enabled" : "Off"}
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
      className: "text-right",
    },
  ];

  const totpPct =
    counts.total === 0 ? 0 : Math.round((counts.totp / counts.total) * 100);

  return (
    <div className="flex flex-col">
      <PageHeader
        title="User Management"
        subtitle={`${users.length} user${users.length === 1 ? "" : "s"} · panel access`}
        trailing={
          <Button size="sm" onClick={onAdd}>
            Add User
          </Button>
        }
      />

      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        <PulseRow
          ticks={[
            {
              label: "Total",
              value: counts.total.toLocaleString(),
              hint: `${counts.viewer} viewer${counts.viewer === 1 ? "" : "s"}`,
            },
            {
              label: "Admins",
              value: counts.admin.toLocaleString(),
              hint: counts.admin === 0 ? "no admins" : "full access",
              tone: counts.admin === 0 ? "error" : "default",
            },
            {
              label: "Operators",
              value: counts.operator.toLocaleString(),
              hint: "mutate nodes & clients",
            },
            {
              label: "2FA coverage",
              value: `${totpPct}%`,
              hint: `${counts.totp}/${counts.total} enrolled`,
              tone:
                counts.total === 0
                  ? "default"
                  : totpPct === 100
                    ? "ok"
                    : totpPct >= 50
                      ? "warn"
                      : "error",
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
                All
              </FilterChip>
              <FilterChip
                active={roleFilter === "admin"}
                onClick={() => setRoleFilter("admin")}
                count={counts.admin}
              >
                Admin
              </FilterChip>
              <FilterChip
                active={roleFilter === "operator"}
                onClick={() => setRoleFilter("operator")}
                count={counts.operator}
              >
                Operator
              </FilterChip>
              <FilterChip
                active={roleFilter === "viewer"}
                onClick={() => setRoleFilter("viewer")}
                count={counts.viewer}
              >
                Viewer
              </FilterChip>
            </>
          }
          search={{
            value: query,
            onChange: setQuery,
            placeholder: "Search username…",
          }}
        />

        {filtered.length === 0 ? (
          <EmptyState
            title={users.length === 0 ? "No panel users yet" : "No users match the filter"}
            description={
              users.length === 0
                ? "Add the first panel user to grant dashboard access. Admins can manage everything; operators can mutate fleets; viewers are read-only."
                : "Widen the filter or clear the search."
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
