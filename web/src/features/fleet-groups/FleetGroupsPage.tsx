import { useTranslation } from "react-i18next";

import {
  Badge,
  Button,
  DataTable,
  EmptyState,
  MonoValue,
  PageHeader,
  Sheet,
  SheetBody,
  SheetContent,
} from "@/ui";
import type { FleetGroupEntry } from "@/shared/api/api";

import { FleetGroupFormSheet, type FleetGroupFormData } from "./FleetGroupFormSheet";

interface FleetGroupsPageProps {
  groups: FleetGroupEntry[];
  sheet:
    | { mode: "closed" }
    | { mode: "create" }
    | { mode: "edit"; id: string };
  formData: FleetGroupFormData;
  formError: string;
  onFormDataChange: (data: Readonly<FleetGroupFormData>) => void;
  onCreate: () => void;
  onEdit: (id: string) => void;
  onOpenDetail: (id: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
  saving: boolean;
}

export function FleetGroupsPage({
  groups,
  sheet,
  formData,
  formError,
  onFormDataChange,
  onCreate,
  onEdit,
  onOpenDetail,
  onSubmit,
  onCancel,
  saving,
}: Readonly<FleetGroupsPageProps>) {
  const { t } = useTranslation("fleet-groups");

  const columns = [
    {
      key: "label",
      header: t("table.label"),
      render: (g: Readonly<FleetGroupEntry>) => (
        <div className="flex flex-col min-w-0">
          <span className="font-medium text-fg truncate">{g.label || g.name}</span>
          <span className="text-[11px] font-mono text-fg-muted truncate">{g.name}</span>
        </div>
      ),
      className: "w-[40%]",
    },
    {
      key: "agents",
      header: t("table.agents"),
      render: (g: Readonly<FleetGroupEntry>) => (
        <MonoValue className="text-fg">{g.agent_count}</MonoValue>
      ),
      className: "w-[90px] text-center",
    },
    {
      key: "integrations",
      header: t("table.integrations"),
      render: (g: Readonly<FleetGroupEntry>) => {
        const count = g.integrations?.length ?? 0;
        return count > 0 ? (
          <Badge variant="default">{count}</Badge>
        ) : (
          <span className="text-[11px] font-mono text-fg-muted">—</span>
        );
      },
      className: "hidden md:table-cell w-[120px]",
    },
    {
      key: "description",
      header: t("table.description"),
      render: (g: Readonly<FleetGroupEntry>) => (
        <span className="text-sm text-fg-muted line-clamp-1">
          {g.description || "—"}
        </span>
      ),
      className: "hidden lg:table-cell",
    },
  ];

  return (
    <>
      <PageHeader
        title={t("page.title")}
        subtitle={t("page.count", { count: groups.length })}
        trailing={
          <Button size="sm" onClick={onCreate}>
            {t("page.new")}
          </Button>
        }
      />

      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        {groups.length === 0 ? (
          <EmptyState
            icon="🗂"
            title={t("empty.title")}
            description={t("empty.description")}
          />
        ) : (
          <div className="bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
            <DataTable
              columns={[
                ...columns,
                {
                  key: "actions",
                  header: "",
                  render: (g: Readonly<FleetGroupEntry>) => (
                    <div className="flex gap-1 justify-end">
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={(e) => {
                          e.stopPropagation();
                          onEdit(g.id);
                        }}
                      >
                        {t("table.edit")}
                      </Button>
                    </div>
                  ),
                  className: "w-[110px] text-right",
                },
              ]}
              data={groups}
              keyExtractor={(g) => g.id}
              onRowClick={(g) => onOpenDetail(g.id)}
            />
          </div>
        )}
      </div>

      <Sheet open={sheet.mode !== "closed"} onOpenChange={(open) => { if (!open) onCancel(); }}>
        <SheetContent
          side="bottom"
          title={sheet.mode === "edit" ? t("form.editTitle") : t("form.createTitle")}
          onOpenChange={(open) => { if (!open) onCancel(); }}
        >
          <SheetBody>
            <FleetGroupFormSheet
              mode={sheet.mode === "edit" ? "edit" : "create"}
              data={formData}
              onChange={onFormDataChange}
              onSubmit={onSubmit}
              onCancel={onCancel}
              loading={saving}
              error={formError || undefined}
            />
          </SheetBody>
        </SheetContent>
      </Sheet>
    </>
  );
}
