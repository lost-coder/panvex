import { useEffect, useId, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Button, FormField, Input } from "@/ui";
import { Sheet, SheetBody, SheetContent, SheetHeader, SheetTitle } from "@/ui/base/sheet";
import type { WebhookEndpoint } from "@/shared/api/webhooks";
import {
  createWebhookEndpointRequestSchema,
  updateWebhookEndpointRequestSchema,
} from "@/shared/api/schemas/requests";

export type WebhookFormMode = "create" | "edit";

export type WebhookFormData = {
  name: string;
  url: string;
  secret: string;
  event_filter: string;
  allow_private: boolean;
  enabled: boolean;
};

export const emptyWebhookForm: WebhookFormData = {
  name: "",
  url: "https://",
  secret: "",
  event_filter: "",
  allow_private: false,
  enabled: true,
};

export function webhookFormFromEndpoint(ep: WebhookEndpoint): WebhookFormData {
  return {
    name: ep.name,
    url: ep.url,
    secret: "",
    event_filter: ep.event_filter,
    allow_private: ep.allow_private,
    enabled: ep.enabled,
  };
}

export interface WebhookFormSheetProps {
  mode: WebhookFormMode;
  data: WebhookFormData;
  onChange: (next: WebhookFormData) => void;
  onSubmit: () => void;
  onCancel: () => void;
  loading?: boolean;
  error?: string;
}

// 7.6: on-form валидация ВЫВОДИТСЯ из канонических Zod request-схем
// (webhookEndpointRequest.ts) вместо ручного зеркала — правило, меняясь
// в схеме, автоматически подтягивается сюда. Схемы не локализованы,
// поэтому маппинг issue→сообщение (по полю) остаётся на форме. Бонус к
// старому зеркалу: клиентски ловятся url≤2048 и синтаксис event_filter.
function validate(mode: WebhookFormMode, data: WebhookFormData, t: TFunction): string | null {
  const schema =
    mode === "create"
      ? createWebhookEndpointRequestSchema
      : updateWebhookEndpointRequestSchema;
  const result = schema.safeParse({
    ...data,
    name: data.name.trim(),
    url: data.url.trim(),
    secret: data.secret.trim(),
  });
  if (result.success) return null;
  const issue = result.error.issues[0];
  switch (issue?.path[0]) {
    case "name":
      return issue.code === "too_big"
        ? t("webhooks.form.validation.nameTooLong")
        : t("webhooks.form.validation.nameRequired");
    case "url":
      return t("webhooks.form.validation.urlScheme");
    case "secret":
      return issue.code === "too_big"
        ? t("webhooks.form.validation.secretTooLong")
        : t("webhooks.form.validation.secretRequired");
    case "event_filter":
      return t("webhooks.form.validation.filterInvalid");
    default:
      return t("webhooks.form.validation.invalid");
  }
}

export function WebhookFormSheet({
  mode,
  data,
  onChange,
  onSubmit,
  onCancel,
  loading,
  error,
}: Readonly<WebhookFormSheetProps>) {
  const { t } = useTranslation("settings");
  const [localError, setLocalError] = useState<string | null>(null);
  const enabledId = useId();
  const allowPrivateId = useId();

  // Clear the local validation hint as the operator types — the server
  // error stays visible until they retry.
  useEffect(() => {
    setLocalError(null);
  }, [data]);

  function update<K extends keyof WebhookFormData>(key: K, value: WebhookFormData[K]) {
    onChange({ ...data, [key]: value });
  }

  function handleSubmit() {
    const v = validate(mode, data, t);
    if (v) {
      setLocalError(v);
      return;
    }
    setLocalError(null);
    onSubmit();
  }

  const submitLabel = (() => {
    if (loading) return t("webhooks.form.saving");
    if (mode === "create") return t("webhooks.form.create");
    return t("webhooks.form.save");
  })();

  const visibleError = localError ?? error;

  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <SheetContent>
        <SheetHeader>
          <SheetTitle>{mode === "create" ? t("webhooks.form.createTitle") : t("webhooks.form.editTitle")}</SheetTitle>
        </SheetHeader>
        <SheetBody>
          <div className="flex flex-col gap-4">
            <p className="text-sm text-fg-muted">
              <Trans i18nKey="webhooks.form.intro" ns="settings" components={{ 1: <code /> }} />
            </p>

            <FormField label={t("webhooks.form.nameLabel")} variant="uppercase" required>
              <Input
                value={data.name}
                onChange={(e) => update("name", e.target.value)}
                placeholder={t("webhooks.form.namePlaceholder")}
                disabled={loading}
                autoComplete="off"
                maxLength={128}
              />
            </FormField>

            <FormField label={t("webhooks.form.urlLabel")} variant="uppercase" required>
              <Input
                value={data.url}
                onChange={(e) => update("url", e.target.value)}
                placeholder={t("webhooks.form.urlPlaceholder")}
                disabled={loading}
                autoComplete="off"
                inputMode="url"
              />
            </FormField>

            <FormField
              label={mode === "create" ? t("webhooks.form.secretLabel") : t("webhooks.form.secretLabelEdit")}
              variant="uppercase"
              description={t("webhooks.form.secretDescription")}
              required={mode === "create"}
            >
              <Input
                type="password"
                value={data.secret}
                onChange={(e) => update("secret", e.target.value)}
                placeholder={mode === "edit" ? t("webhooks.form.secretPlaceholderEdit") : ""}
                disabled={loading}
                autoComplete="new-password"
                maxLength={1024}
              />
            </FormField>

            <FormField
              label={t("webhooks.form.filterLabel")}
              variant="uppercase"
              description={t("webhooks.form.filterDescription")}
            >
              <Input
                value={data.event_filter}
                onChange={(e) => update("event_filter", e.target.value)}
                placeholder={t("webhooks.form.filterPlaceholder")}
                disabled={loading}
                autoComplete="off"
              />
            </FormField>

            <div className="flex items-start gap-2 text-sm text-fg">
              <input
                id={enabledId}
                type="checkbox"
                className="h-4 w-4 mt-0.5 accent-[var(--color-accent)] cursor-pointer"
                checked={data.enabled}
                onChange={(e) => update("enabled", e.target.checked)}
                disabled={loading}
              />
              <label htmlFor={enabledId} className="flex flex-col cursor-pointer">
                <span>{t("webhooks.form.enabledLabel")}</span>
                <span className="text-caption">{t("webhooks.form.enabledHint")}</span>
              </label>
            </div>

            <div className="flex items-start gap-2 text-sm text-fg">
              <input
                id={allowPrivateId}
                type="checkbox"
                className="h-4 w-4 mt-0.5 accent-[var(--color-accent)] cursor-pointer"
                checked={data.allow_private}
                onChange={(e) => update("allow_private", e.target.checked)}
                disabled={loading}
              />
              <label htmlFor={allowPrivateId} className="flex flex-col cursor-pointer">
                <span>{t("webhooks.form.allowPrivateLabel")}</span>
                <span className="text-caption">{t("webhooks.form.allowPrivateHint")}</span>
              </label>
            </div>

            {visibleError && <div className="text-xs text-status-error">{visibleError}</div>}

            <div className="flex gap-2 justify-end mt-2">
              <Button variant="ghost" onClick={onCancel} disabled={loading}>
                {t("webhooks.form.cancel")}
              </Button>
              <Button onClick={handleSubmit} disabled={loading}>
                {submitLabel}
              </Button>
            </div>
          </div>
        </SheetBody>
      </SheetContent>
    </Sheet>
  );
}
