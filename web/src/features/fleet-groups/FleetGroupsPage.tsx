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
          <span className="text-micro font-mono text-fg-muted truncate">{g.name}</span>
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
          <span className="text-micro font-mono text-fg-muted">—</span>
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
          <>
            {/* Mobile (U-14): one card per group — name as the heading,
                metrics on their own line right under it, tap opens detail,
                Edit is a real button. The DataTable's auto label-value
                collapse pushed values 200px from their labels on 390px. */}
            <div className="md:hidden flex flex-col gap-3">
              {groups.map((g) => {
                const integrations = g.integrations?.length ?? 0;
                return (
                  <button
                    key={g.id}
                    type="button"
                    onClick={() => onOpenDetail(g.id)}
                    className="text-left rounded-xl bg-bg-card border border-border p-4 shadow-sm transition-colors hover:bg-bg-hover hover:border-border-hi focus-visible:outline-2 focus-visible:outline-accent"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex flex-col min-w-0">
                        <span className="font-medium text-fg truncate">{g.label || g.name}</span>
                        <span className="text-micro font-mono text-fg-muted truncate">{g.name}</span>
                      </div>
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
                    <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-micro font-mono text-fg-muted">
                      <span>{t("table.agents")}: <span className="text-fg">{g.agent_count}</span></span>
                      {integrations > 0 && (
                        <span>{t("table.integrations")}: <span className="text-fg">{integrations}</span></span>
                      )}
                    </div>
                    {g.description && (
                      <p className="mt-2 text-sm text-fg-muted line-clamp-2">{g.description}</p>
                    )}
                  </button>
                );
              })}
            </div>

            {/* Desktop: the existing dense table. */}
            <div className="hidden md:block bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
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
          </>
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
