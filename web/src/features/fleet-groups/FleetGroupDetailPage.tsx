import { useTranslation } from "react-i18next";

import {
  Badge,
  Breadcrumbs,
  Button,
  KvGrid,
  PageHeader,
  Sheet,
  SheetBody,
  SheetContent,
} from "@/ui";
import type { FleetGroupEntry } from "@/shared/api/api";

import { FleetGroupFormSheet, type FleetGroupFormData } from "./FleetGroupFormSheet";

interface FleetGroupDetailPageProps {
  group: FleetGroupEntry;
  onBack: () => void;
  onEdit: () => void;
  onDelete: () => void;
  editOpen: boolean;
  formData: FleetGroupFormData;
  formError: string;
  onFormDataChange: (data: Readonly<FleetGroupFormData>) => void;
  onSubmitEdit: () => void;
  onCancelEdit: () => void;
  saving: boolean;
}

export function FleetGroupDetailPage({
  group,
  onBack,
  onEdit,
  onDelete,
  editOpen,
  formData,
  formError,
  onFormDataChange,
  onSubmitEdit,
  onCancelEdit,
  saving,
}: Readonly<FleetGroupDetailPageProps>) {
  const { t } = useTranslation("fleet-groups");
  const hasIntegrations = (group.integrations ?? []).length > 0;

  return (
    <>
      <div className="px-4 md:px-8 pt-3 pb-3">
        <Breadcrumbs
          items={[
            { label: t("page.title"), onClick: onBack },
            { label: group.label || group.name },
          ]}
        />
      </div>

      <PageHeader
        title={group.label || group.name}
        subtitle={t("detail.subtitle", {
          count: group.agent_count,
          slug: group.name,
        })}
        trailing={
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={onEdit}>
              {t("detail.edit")}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={onDelete}
              className="text-status-error hover:text-status-error"
            >
              {t("detail.delete")}
            </Button>
          </div>
        }
      />

      <div className="px-4 md:px-8 pb-8 pt-4 flex flex-col gap-5">
        <section className="rounded-xs bg-bg-card border border-divider p-4">
          <span className="text-sm font-semibold text-fg mb-3 block">{t("detail.details")}</span>
          <KvGrid
            rows={[
              { label: t("detail.slug"), value: <span className="font-mono text-xs">{group.name}</span> },
              { label: t("detail.label"), value: group.label || "—" },
              { label: t("detail.description"), value: group.description || "—" },
              { label: t("detail.agents"), value: group.agent_count.toString() },
              {
                label: t("detail.created"),
                value: new Date(group.created_at_unix * 1000).toLocaleString(),
              },
              {
                label: t("detail.updated"),
                value: new Date(group.updated_at_unix * 1000).toLocaleString(),
              },
            ]}
          />
        </section>

        <section className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-semibold text-fg">{t("detail.integrations")}</span>
            {hasIntegrations && <Badge variant="default">{group.integrations.length}</Badge>}
          </div>
          {hasIntegrations ? (
            <ul className="flex flex-col gap-2">
              {group.integrations.map((i) => (
                <li
                  key={i.id}
                  className="flex items-center justify-between rounded-xs border border-border/60 px-3 py-2"
                >
                  <div className="flex flex-col">
                    <span className="text-sm text-fg font-mono">{i.kind}</span>
                    <span className="text-[11px] text-fg-muted">
                      {i.enabled ? t("detail.enabled") : t("detail.disabled")}
                      {i.provider_id ? t("detail.providerSuffix", { id: i.provider_id.slice(0, 8) }) : ""}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-xs text-fg-muted">{t("detail.noIntegrations")}</p>
          )}
        </section>
      </div>

      <Sheet open={editOpen} onOpenChange={(open) => { if (!open) onCancelEdit(); }}>
        <SheetContent
          side="bottom"
          title={t("form.editTitle")}
          onOpenChange={(open) => { if (!open) onCancelEdit(); }}
        >
          <SheetBody>
            <FleetGroupFormSheet
              mode="edit"
              data={formData}
              onChange={onFormDataChange}
              onSubmit={onSubmitEdit}
              onCancel={onCancelEdit}
              loading={saving}
              error={formError || undefined}
            />
          </SheetBody>
        </SheetContent>
      </Sheet>
    </>
  );
}
