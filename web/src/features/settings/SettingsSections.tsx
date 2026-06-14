// Section components split out of SettingsPage.tsx to keep that file focused
// on page-level composition. AdminBadge is shared by several admin-gated
// sections; SystemInfoSection and RetentionSection are self-contained
// schema/draft sections.
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Database, Info, ShieldCheck } from "lucide-react";
import { Button, Input, PageSection, Select, SettingsRow } from "@/ui";
import { secondsToDisplay, displayToSeconds } from "@/shared/lib/pages-shared";
import type { SettingsPageProps, SettingsRegistryProps } from "@/shared/api/types-pages/pages";
import { RegistryField, namespaceOf, labelFor } from "@/features/settings/registry";

// Compact "Admin" pill reused across admin-gated sections.
export function AdminBadge() {
  const { t } = useTranslation("settings");
  return (
    <span className="inline-flex items-center gap-1 rounded-xs border border-accent/20 bg-accent/5 px-1.5 py-0.5 text-pico font-mono uppercase tracking-wider text-accent">
      <ShieldCheck className="h-2.5 w-2.5" aria-hidden />
      {t("page.adminBadge")}
    </span>
  );
}

// ─── SystemInfo ───────────────────────────────────────────────────────────────

export function SystemInfoSection({ registry }: Readonly<{ registry: SettingsRegistryProps }>) {
  const { t } = useTranslation("settings");
  // Collect all bootstrap schema entries grouped by namespace.
  const byNamespace = new Map<string, typeof registry.schema>();
  for (const s of registry.schema) {
    if (s.class !== "bootstrap") continue;
    const ns = namespaceOf(s.name);
    const existing = byNamespace.get(ns) ?? [];
    existing.push(s);
    byNamespace.set(ns, existing);
  }

  return (
    <div className="md:col-span-2">
      <PageSection
        icon={Info}
        title={t("systemInfo.title")}
        description={t("systemInfo.description")}
      >
        {Array.from(byNamespace.entries()).map(([ns, fields]) => {
          const label = labelFor(ns, t);
          return (
            <div key={ns}>
              <h3 className="px-4 pt-3 pb-1 text-xs font-mono uppercase tracking-wider text-fg-muted">
                {label.title}
              </h3>
              {fields.map((s) => {
                const entry = registry.values[s.name] ?? {
                  value: "",
                  source: "default" as const,
                  locked: true,
                };
                return (
                  <RegistryField
                    key={s.name}
                    schema={s}
                    values={{ ...entry, locked: true }}
                    onChange={() => {}}
                    hideIndicators
                  />
                );
              })}
            </div>
          );
        })}
      </PageSection>
    </div>
  );
}

// ─── Retention ────────────────────────────────────────────────────────────────

const RETENTION_FIELD_KEYS: ReadonlyArray<
  keyof NonNullable<SettingsPageProps["retentionSettings"]>
> = [
  "ts_raw_seconds",
  "ts_hourly_seconds",
  "ts_dc_seconds",
  "ip_history_seconds",
  "event_history_seconds",
];

function unitOptions(t: TFunction) {
  return [
    { value: "seconds", label: t("retention.units.seconds") },
    { value: "minutes", label: t("retention.units.minutes") },
    { value: "hours", label: t("retention.units.hours") },
    { value: "days", label: t("retention.units.days") },
  ];
}

export function RetentionSection({
  settings,
  onChange,
  saving,
}: Readonly<{
  settings: NonNullable<SettingsPageProps["retentionSettings"]>;
  onChange: (s: Readonly<NonNullable<SettingsPageProps["retentionSettings"]>>) => void;
  saving?: boolean | undefined;
}>) {
  const { t } = useTranslation("settings");
  const [draft, setDraft] = useState(settings);
  const isDirty = JSON.stringify(draft) !== JSON.stringify(settings);
  const units = unitOptions(t);

  function updateField(key: keyof typeof draft, value: number, unit: string) {
    setDraft((prev) => ({ ...prev, [key]: displayToSeconds(value, unit) }));
  }

  return (
    <PageSection
      icon={Database}
      title={t("retention.title")}
      description={t("retention.description")}
      badge={<AdminBadge />}
    >
      {RETENTION_FIELD_KEYS.map((key) => {
        const display = secondsToDisplay(draft[key]);
        return (
          <SettingsRow
            key={key}
            label={t(`retention.fields.${key}.label`)}
            description={t(`retention.fields.${key}.description`)}
          >
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={1}
                value={display.value}
                onChange={(e) => updateField(key, Number(e.target.value) || 1, display.unit)}
                className="w-20"
              />
              <Select
                value={display.unit}
                onChange={(v) => updateField(key, display.value, v)}
                options={units}
              />
            </div>
          </SettingsRow>
        );
      })}
      {isDirty && (
        <div className="flex items-center justify-between px-4 py-3 bg-accent/5">
          <span className="text-xs font-mono text-fg-muted">
            {t("retention.unsavedNotice")}
          </span>
          <div className="flex gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setDraft(settings)}
              disabled={saving}
            >
              {t("retention.cancel")}
            </Button>
            <Button size="sm" onClick={() => onChange(draft)} disabled={saving}>
              {saving ? t("retention.saving") : t("retention.save")}
            </Button>
          </div>
        </div>
      )}
    </PageSection>
  );
}
