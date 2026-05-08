import { useState } from "react";
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
  return (
    <span className="inline-flex items-center gap-1 rounded-xs border border-accent/20 bg-accent/5 px-1.5 py-0.5 text-[9px] font-mono uppercase tracking-wider text-accent">
      <ShieldCheck className="h-2.5 w-2.5" aria-hidden />
      Admin
    </span>
  );
}

function describeFilter(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) return "all events";
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
}

function WebhookRow({ endpoint, onEdit, onDelete }: Readonly<RowProps>) {
  return (
    <div className="flex items-start justify-between gap-3 px-4 py-3 border-t border-border first:border-t-0">
      <div className="flex flex-col gap-1 min-w-0 flex-1">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-sm font-medium text-fg truncate">{endpoint.name}</span>
          <StatusLabel
            tone={endpoint.enabled ? "ok" : "default"}
            label={endpoint.enabled ? "Enabled" : "Disabled"}
          />
          {endpoint.allow_private && (
            <span className="inline-flex items-center rounded-xs border border-status-warn/30 bg-status-warn/10 px-1.5 py-0.5 text-[9px] font-mono uppercase tracking-wider text-status-warn">
              Private OK
            </span>
          )}
        </div>
        <span className="text-xs font-mono text-fg-muted truncate">{endpoint.url}</span>
        <span className="text-[11px] text-fg-muted truncate">
          Filter: <span className="font-mono">{describeFilter(endpoint.event_filter)}</span>
        </span>
      </div>
      <div className="flex gap-1 shrink-0">
        <Button variant="ghost" size="sm" onClick={() => onEdit(endpoint.id)}>
          Edit
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => onDelete(endpoint.id)}
          aria-label={`Delete ${endpoint.name}`}
          className="text-status-error hover:text-status-error"
        >
          <Trash2 className="h-3.5 w-3.5" aria-hidden />
        </Button>
      </div>
    </div>
  );
}

export function WebhooksSection() {
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
    const name = ep?.name ?? "this endpoint";
    const ok = await confirm({
      title: "Delete webhook endpoint?",
      body: `"${name}" will stop receiving events immediately. Pending outbox rows for this endpoint will also be removed (CASCADE).`,
      confirmLabel: "Delete endpoint",
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
            setFormError(errorMessage(err, "Failed to create webhook endpoint.")),
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
            setFormError(errorMessage(err, "Failed to update webhook endpoint.")),
        },
      );
    }
  }

  return (
    <PageSection
      icon={Webhook}
      title="Webhook Endpoints"
      description="HMAC-signed outbound delivery for audit + alert events. Receivers are matched by event_filter and retried with exponential backoff."
      badge={<AdminBadge />}
    >
      <div className="flex items-center justify-between gap-3 px-4 py-3 border-b border-border">
        <span className="text-xs font-mono text-fg-muted">
          {endpoints.length} endpoint{endpoints.length === 1 ? "" : "s"}
        </span>
        <Button size="sm" onClick={handleAdd}>
          <Plus className="h-3.5 w-3.5 mr-1" aria-hidden />
          Add Webhook
        </Button>
      </div>

      {isLoading && (
        <div className="px-4 py-3">
          <SkeletonRows count={2} />
        </div>
      )}

      {!isLoading && error && (
        <div className="px-4 py-3 text-xs text-status-error">
          {errorMessage(error, "Failed to load webhook endpoints.")}
        </div>
      )}

      {!isLoading && !error && endpoints.length === 0 && (
        <div className="px-4 py-6 text-center">
          <p className="text-sm text-fg-muted">
            No webhook endpoints configured yet.
          </p>
          <p className="text-caption mt-1">
            Add one to start fanning out audit + alert events to Slack, PagerDuty, or your own receiver.
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
