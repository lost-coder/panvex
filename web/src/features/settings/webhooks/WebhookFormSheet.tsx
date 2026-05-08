import { useEffect, useId, useState } from "react";
import { Button, FormField, Input } from "@/ui";
import { Sheet, SheetBody, SheetContent, SheetHeader, SheetTitle } from "@/ui/base/sheet";
import type { WebhookEndpoint } from "@/shared/api/webhooks";

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

// Lightweight on-form validation — surfaces the same rules the server
// enforces so the operator gets immediate feedback before a 400 round
// trip. The Zod request schema in shared/api/schemas/requests/
// webhookEndpointRequest.ts is the canonical source of truth; this
// mirror catches the obvious cases.
function validate(mode: WebhookFormMode, data: WebhookFormData): string | null {
  if (!data.name.trim()) return "Name is required.";
  if (data.name.length > 128) return "Name must be 128 characters or fewer.";
  if (!/^https?:\/\//i.test(data.url.trim())) return "URL must start with http:// or https://.";
  if (mode === "create" && !data.secret.trim()) return "Secret is required.";
  if (data.secret.length > 1024) return "Secret must be 1024 characters or fewer.";
  return null;
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
    const v = validate(mode, data);
    if (v) {
      setLocalError(v);
      return;
    }
    setLocalError(null);
    onSubmit();
  }

  const submitLabel = (() => {
    if (loading) return "Saving…";
    if (mode === "create") return "Add Webhook";
    return "Save Changes";
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
          <SheetTitle>{mode === "create" ? "Add Webhook Endpoint" : "Edit Webhook Endpoint"}</SheetTitle>
        </SheetHeader>
        <SheetBody>
          <div className="flex flex-col gap-4">
            <p className="text-sm text-fg-muted">
              Outbound HTTP receiver for audit + alert events. Bodies are HMAC-SHA256 signed
              with the endpoint secret; receivers verify via the <code>X-Panvex-Signature</code> header.
            </p>

            <FormField label="Name" variant="uppercase" required>
              <Input
                value={data.name}
                onChange={(e) => update("name", e.target.value)}
                placeholder="ops-slack"
                disabled={loading}
                autoComplete="off"
                maxLength={128}
              />
            </FormField>

            <FormField label="URL" variant="uppercase" required>
              <Input
                value={data.url}
                onChange={(e) => update("url", e.target.value)}
                placeholder="https://hooks.example.com/services/…"
                disabled={loading}
                autoComplete="off"
                inputMode="url"
              />
            </FormField>

            <FormField
              label={mode === "create" ? "Secret" : "Secret (leave blank to keep)"}
              variant="uppercase"
              description="HMAC key used to sign delivery bodies. Treated as a credential — not returned by the API after save."
              required={mode === "create"}
            >
              <Input
                type="password"
                value={data.secret}
                onChange={(e) => update("secret", e.target.value)}
                placeholder={mode === "edit" ? "••••••••" : ""}
                disabled={loading}
                autoComplete="new-password"
                maxLength={1024}
              />
            </FormField>

            <FormField
              label="Event filter"
              variant="uppercase"
              description="Comma-separated event actions or prefixes (e.g. `audit.*,alert.fired`). Empty = match all."
            >
              <Input
                value={data.event_filter}
                onChange={(e) => update("event_filter", e.target.value)}
                placeholder="audit.*,alert.fired"
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
                <span>Enabled</span>
                <span className="text-caption">Disabled endpoints stop receiving new events; queued rows drain only when re-enabled.</span>
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
                <span>Allow private destinations</span>
                <span className="text-caption">Bypasses SSRF preflight. Only enable for receivers on operator-owned private networks.</span>
              </label>
            </div>

            {visibleError && <div className="text-xs text-status-error">{visibleError}</div>}

            <div className="flex gap-2 justify-end mt-2">
              <Button variant="ghost" onClick={onCancel} disabled={loading}>
                Cancel
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
