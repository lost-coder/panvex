// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: single-scroll, multi-section page. No rail nav — with only
// 2–6 sections scrolling beats a table-of-contents.
import { useTranslation } from "react-i18next";
import {
  Palette,
  Users as UsersIcon,
  Power,
  Server as ServerIcon,
  ShieldCheck,
  Save,
} from "lucide-react";
import {
  Button,
  Input,
  PageHeader,
  PageSection,
  Select,
  SettingsRow,
} from "@/ui";
import type { SettingsPageProps } from "@/shared/api/types-pages/pages";
import { RestartBanner, RegistrySection, SettingsLegend, namespaceOf } from "@/features/settings/registry";
import type { RegistrySectionField } from "@/features/settings/registry";
import { AdminBadge, RetentionSection, SystemInfoSection } from "@/features/settings/SettingsSections";
import { useUnsavedChangesGuard } from "@/shared/hooks";

// Operational namespaces rendered as schema-driven sections.
const OPERATIONAL_NAMESPACES = ["http", "agents", "auth", "jobs", "observability", "storage"] as const;

export function SettingsPage({
  panelSettings,
  appearanceSettings,
  onPanelSettingsChange,
  onAppearanceChange,
  onRestart,
  onManageUsers,
  retentionSettings,
  onRetentionChange,
  retentionSaving,
  registry,
  children,
}: Readonly<SettingsPageProps>) {
  const { t } = useTranslation("settings");
  const hasAdmin = !!(onManageUsers || (retentionSettings && onRetentionChange) || onRestart);

  // Audit E4: guard in-app navigation while the registry has unsaved drafts.
  useUnsavedChangesGuard(Boolean(registry?.isDirty));

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("page.title")}
        subtitle={t("page.subtitle")}
        trailing={
          hasAdmin ? (
            <span className="inline-flex items-center gap-1.5 rounded-xs border border-accent/30 bg-accent/10 px-2 py-1 text-nano font-mono uppercase tracking-wider text-accent">
              <ShieldCheck className="h-3 w-3" aria-hidden />
              {t("page.adminBadge")}
            </span>
          ) : (
            <span className="inline-flex items-center gap-1.5 rounded-xs border border-border bg-bg-card px-2 py-1 text-nano font-mono uppercase tracking-wider text-fg-muted">
              {t("page.userBadge")}
            </span>
          )
        }
      />

      {/* Pending-restart banner — shown above content when registry detects fields
          that need a restart before their new value takes effect. */}
      {registry && registry.pendingRestart.length > 0 && (
        <div className="px-4 md:px-8 pt-4">
          <RestartBanner
            pendingFields={registry.pendingRestart}
            onRestart={registry.onRestart}
            restartInFlight={registry.isRestartInFlight}
          />
        </div>
      )}

      <div className="px-4 md:px-8 pb-8">
        {/* Registry Save/Cancel toolbar — floats above the grid when there are
            unsaved schema-driven changes. */}
        {registry && registry.isDirty && (
          <div className="flex items-center justify-between gap-4 py-3 mb-2 border-b border-border">
            <span className="text-xs font-mono text-fg-muted">
              {t("registry.unsaved", { count: Object.keys(registry.draft).length })}
            </span>
            <div className="flex gap-2">
              <Button
                variant="ghost"
                size="sm"
                onClick={registry.onCancelDraft}
                disabled={registry.isSaving}
              >
                {t("registry.cancel")}
              </Button>
              <Button
                size="sm"
                onClick={registry.onSave}
                disabled={registry.isSaving}
              >
                <Save className="h-3.5 w-3.5 mr-1" aria-hidden />
                {registry.isSaving ? t("registry.saving") : t("registry.save")}
              </Button>
            </div>
          </div>
        )}

        {registry && (
          <div className="mb-4">
            <SettingsLegend />
          </div>
        )}

        {/* 2-col grid on desktop keeps both halves of the viewport busy:
            short sections (System, Users) sit next to longer ones (Retention,
            Appearance) instead of leaving empty half-screen real estate. */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-8 items-start">

            {/* Schema-driven operational sections — one per namespace. */}
            {registry && OPERATIONAL_NAMESPACES.map((ns) => {
              const nsFields: RegistrySectionField[] = registry.schema
                .filter(
                  (s) =>
                    s.class === "operational" &&
                    namespaceOf(s.name) === ns,
                )
                .map((s) => {
                  const err = registry.errors[s.name];
                  const field: RegistrySectionField = {
                    schema: s,
                    values: registry.values[s.name] ?? { value: "", source: "default" as const, locked: false },
                    ...(err !== undefined ? { error: err } : {}),
                  };
                  return field;
                });
              if (nsFields.length === 0) return null;
              return (
                <RegistrySection
                  key={ns}
                  namespace={ns}
                  fields={nsFields}
                  onChange={registry.onDraftChange}
                />
              );
            })}

            <PageSection
              icon={ServerIcon}
              title={t("panel.title")}
              description={t("panel.description")}
            >
              <SettingsRow
                label={t("panel.httpPublicUrlLabel")}
                description={t("panel.httpPublicUrlDescription")}
              >
                <Input
                  className="w-64"
                  value={panelSettings.httpPublicUrl}
                  placeholder={t("panel.httpPublicUrlPlaceholder")}
                  onChange={(e) =>
                    onPanelSettingsChange?.({
                      ...panelSettings,
                      httpPublicUrl: e.target.value,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow label={t("panel.grpcEndpointLabel")} description={t("panel.grpcEndpointDescription")}>
                <Input
                  className="w-64"
                  value={panelSettings.grpcPublicEndpoint}
                  placeholder={t("panel.grpcEndpointPlaceholder")}
                  onChange={(e) =>
                    onPanelSettingsChange?.({
                      ...panelSettings,
                      grpcPublicEndpoint: e.target.value,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow
                label={t("panel.passwordMinLengthLabel")}
                description={t("panel.passwordMinLengthDescription")}
              >
                <Input
                  type="number"
                  className="w-24"
                  min={8}
                  max={64}
                  step={1}
                  value={panelSettings.passwordMinLength}
                  onChange={(e) =>
                    onPanelSettingsChange?.({
                      ...panelSettings,
                      passwordMinLength: Number(e.target.value) || 8,
                    })
                  }
                  aria-label={t("panel.passwordMinLengthAriaLabel")}
                />
              </SettingsRow>
            </PageSection>

            <PageSection
              icon={Palette}
              title={t("appearance.title")}
              description={t("appearance.description")}
            >
              <SettingsRow label={t("appearance.themeLabel")}>
                <Select
                  className="w-36"
                  value={appearanceSettings.theme}
                  options={[
                    { value: "system", label: t("appearance.themeOptions.system") },
                    { value: "light", label: t("appearance.themeOptions.light") },
                    { value: "dark", label: t("appearance.themeOptions.dark") },
                  ]}
                  onChange={(v) =>
                    onAppearanceChange?.({
                      ...appearanceSettings,
                      theme: v as typeof appearanceSettings.theme,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow label={t("appearance.densityLabel")}>
                <Select
                  className="w-36"
                  value={appearanceSettings.density}
                  options={[
                    { value: "comfortable", label: t("appearance.densityOptions.comfortable") },
                    { value: "compact", label: t("appearance.densityOptions.compact") },
                  ]}
                  onChange={(v) =>
                    onAppearanceChange?.({
                      ...appearanceSettings,
                      density: v as typeof appearanceSettings.density,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow label={t("appearance.helpModeLabel")}>
                <Select
                  className="w-36"
                  value={appearanceSettings.helpMode}
                  options={[
                    { value: "off", label: t("appearance.helpModeOptions.off") },
                    { value: "basic", label: t("appearance.helpModeOptions.basic") },
                    { value: "full", label: t("appearance.helpModeOptions.full") },
                  ]}
                  onChange={(v) =>
                    onAppearanceChange?.({
                      ...appearanceSettings,
                      helpMode: v as typeof appearanceSettings.helpMode,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow label={t("appearance.swipeNavigationLabel")} description={t("appearance.swipeNavigationDescription")}>
                <input
                  type="checkbox"
                  className="h-4 w-4 accent-[var(--color-accent)] cursor-pointer"
                  checked={appearanceSettings.swipeNavigation}
                  onChange={(e) =>
                    onAppearanceChange?.({
                      ...appearanceSettings,
                      swipeNavigation: e.target.checked,
                    })
                  }
                />
              </SettingsRow>
            </PageSection>

            {/* Admin-only sections below. Each is gated individually so if a
                prop is absent the section disappears cleanly instead of
                rendering an empty card. */}

            {hasAdmin && children}

            {onManageUsers && (
              <PageSection
                icon={UsersIcon}
                title={t("users.title")}
                description={t("users.description")}
                badge={<AdminBadge />}
              >
                <SettingsRow
                  label={t("users.accountsLabel")}
                  description={t("users.accountsDescription")}
                >
                  <Button size="sm" onClick={onManageUsers}>
                    {t("users.manageButton")}
                  </Button>
                </SettingsRow>
              </PageSection>
            )}

            {retentionSettings && onRetentionChange && (
              <RetentionSection
                settings={retentionSettings}
                onChange={onRetentionChange}
                saving={retentionSaving}
              />
            )}

            {onRestart && (
              <PageSection
                icon={Power}
                title={t("system.title")}
                description={t("system.description")}
                badge={<AdminBadge />}
                tone="danger"
              >
                <SettingsRow
                  label={t("system.restartLabel")}
                  description={t("system.restartDescription")}
                >
                  <Button variant="danger" size="sm" onClick={onRestart}>
                    {t("system.restartButton")}
                  </Button>
                </SettingsRow>
              </PageSection>
            )}

            {/* System Info — bootstrap fields, all locked (read-only). Grouped by
                namespace using subheadings inside a single collapsible section. */}
            {registry && registry.schema.some((s) => s.class === "bootstrap") && (
              <SystemInfoSection registry={registry} />
            )}

        </div>
      </div>
    </div>
  );
}
