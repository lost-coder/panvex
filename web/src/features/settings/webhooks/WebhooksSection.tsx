import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Plus, ShieldCheck, Trash2, Webhook } from "lucide-react";

import { Button, PageSection, SkeletonRows, StatusLabel } from "@/ui";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import type { WebhookEndpoint } from "@/shared/api/webhooks";
import { ApiError } from "@/shared/api/api";

import { useWebhooks } from "./useWebhooks";
import {
  WebhookFormSheet,
  emptyWebhookForm,
  webhookFormFromEndpoint,
  type WebhookFormData,
} from "./WebhookFormSheet";

type SheetState =
  | { mode: "closed" }
  | { mode: "create" }
  | { mode: "edit"; endpointId: string };

function AdminBadge() {
  const { t } = useTranslation("settings");
  return (
    <span className="inline-flex items-center gap-1 rounded-xs border border-accent/20 bg-accent/5 px-1.5 py-0.5 text-pico font-mono uppercase tracking-wider text-accent">
      <ShieldCheck className="h-2.5 w-2.5" aria-hidden />
      {t("webhooks.adminBadge")}
    </span>
  );
}

function describeFilter(raw: string, allEventsLabel: string): string {
  const trimmed = raw.trim();
  if (!trimmed) return allEventsLabel;
  return trimmed;
}

function errorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return fallback;
}

interface RowProps {
  endpoint: WebhookEndpoint;
  onEdit: (id: string) => void;
  onDelete: (id: string) => void;
  t: TFunction;
}

function WebhookRow({ endpoint, onEdit, onDelete, t }: Readonly<RowProps>) {
  return (
    <div className="flex items-start justify-between gap-3 px-4 py-3 border-t border-border first:border-t-0">
      <div className="flex flex-col gap-1 min-w-0 flex-1">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-sm font-medium text-fg truncate">{endpoint.name}</span>
          <StatusLabel
            tone={endpoint.enabled ? "ok" : "default"}
            label={endpoint.enabled ? t("webhooks.row.enabled") : t("webhooks.row.disabled")}
          />
          {endpoint.allow_private && (
            <span className="inline-flex items-center rounded-xs border border-status-warn/30 bg-status-warn/10 px-1.5 py-0.5 text-pico font-mono uppercase tracking-wider text-status-warn">
              {t("webhooks.row.allowPrivate")}
            </span>
          )}
        </div>
        <span className="text-xs font-mono text-fg-muted truncate">{endpoint.url}</span>
        <span className="text-micro text-fg-muted truncate">
          {t("webhooks.row.filterPrefix")}{" "}
          <span className="font-mono">{describeFilter(endpoint.event_filter, t("webhooks.row.allEvents"))}</span>
        </span>
      </div>
      <div className="flex gap-1 shrink-0">
        <Button variant="ghost" size="sm" onClick={() => onEdit(endpoint.id)}>
          {t("webhooks.row.edit")}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => onDelete(endpoint.id)}
          aria-label={t("webhooks.row.deleteAriaLabel", { name: endpoint.name })}
          className="text-status-error hover:text-status-error"
        >
          <Trash2 className="h-3.5 w-3.5" aria-hidden />
        </Button>
      </div>
    </div>
  );
}

export function WebhooksSection() {
  const { t } = useTranslation("settings");
  const { endpoints, isLoading, error, createWebhook, updateWebhook, deleteWebhook } = useWebhooks();
  const confirm = useConfirm();

  const [sheet, setSheet] = useState<SheetState>({ mode: "closed" });
  const [formData, setFormData] = useState<WebhookFormData>(emptyWebhookForm);
  const [formError, setFormError] = useState("");

  const isMutating = createWebhook.isPending || updateWebhook.isPending;

  function handleAdd() {
    setFormData(emptyWebhookForm);
    setFormError("");
    setSheet({ mode: "create" });
  }

  function handleEdit(id: string) {
    const ep = endpoints.find((e) => e.id === id);
    if (!ep) return;
    setFormData(webhookFormFromEndpoint(ep));
    setFormError("");
    setSheet({ mode: "edit", endpointId: id });
  }

  async function handleDelete(id: string) {
    const ep = endpoints.find((e) => e.id === id);
    const name = ep?.name ?? t("webhooks.deleteConfirm.fallbackName");
    const ok = await confirm({
      title: t("webhooks.deleteConfirm.title"),
      body: t("webhooks.deleteConfirm.body", { name }),
      confirmLabel: t("webhooks.deleteConfirm.confirm"),
      variant: "danger",
    });
    if (!ok) return;
    deleteWebhook.mutate(id);
  }

  function handleSubmit() {
    setFormError("");
    if (sheet.mode === "create") {
      createWebhook.mutate(
        { ...formData },
        {
          onSuccess: () => setSheet({ mode: "closed" }),
          onError: (err) =>
            setFormError(errorMessage(err, t("webhooks.errors.create"))),
        },
      );
      return;
    }
    if (sheet.mode === "edit") {
      const id = sheet.endpointId;
      updateWebhook.mutate(
        { id, payload: { ...formData } },
        {
          onSuccess: () => setSheet({ mode: "closed" }),
          onError: (err) =>
            setFormError(errorMessage(err, t("webhooks.errors.update"))),
        },
      );
    }
  }

  return (
    <PageSection
      icon={Webhook}
      title={t("webhooks.title")}
      description={t("webhooks.description")}
      badge={<AdminBadge />}
    >
      <div className="flex items-center justify-between gap-3 px-4 py-3 border-b border-border">
        <span className="text-xs font-mono text-fg-muted">
          {t("webhooks.endpoints", { count: endpoints.length })}
        </span>
        <Button size="sm" onClick={handleAdd}>
          <Plus className="h-3.5 w-3.5 mr-1" aria-hidden />
          {t("webhooks.addButton")}
        </Button>
      </div>

      {isLoading && (
        <div className="px-4 py-3">
          <SkeletonRows count={2} />
        </div>
      )}

      {!isLoading && error && (
        <div className="px-4 py-3 text-xs text-status-error">
          {errorMessage(error, t("webhooks.loadError"))}
        </div>
      )}

      {!isLoading && !error && endpoints.length === 0 && (
        <div className="px-4 py-6 text-center">
          <p className="text-sm text-fg-muted">
            {t("webhooks.empty")}
          </p>
          <p className="text-caption mt-1">
            {t("webhooks.emptyHint")}
          </p>
        </div>
      )}

      {!isLoading && !error && endpoints.length > 0 && (
        <div>
          {endpoints.map((ep) => (
            <WebhookRow
              key={ep.id}
              endpoint={ep}
              onEdit={handleEdit}
              onDelete={handleDelete}
              t={t}
            />
          ))}
        </div>
      )}

      {sheet.mode !== "closed" && (
        <WebhookFormSheet
          mode={sheet.mode}
          data={formData}
          onChange={setFormData}
          onSubmit={handleSubmit}
          onCancel={() => setSheet({ mode: "closed" })}
          loading={isMutating}
          error={formError}
        />
      )}
    </PageSection>
  );
}
