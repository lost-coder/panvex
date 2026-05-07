// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: single-scroll, multi-section page. No rail nav — with only
// 2–6 sections scrolling beats a table-of-contents.
import { useState } from "react";
import {
  Palette,
  Users as UsersIcon,
  Database,
  Power,
  Server as ServerIcon,
  ShieldCheck,
  Save,
  Info,
} from "lucide-react";
import {
  Button,
  Input,
  PageHeader,
  PageSection,
  Select,
  SettingsRow,
} from "@/ui";
import { secondsToDisplay, displayToSeconds } from "@/shared/lib/pages-shared";
import type { SettingsPageProps, SettingsRegistryProps } from "@/shared/api/types-pages/pages";
import { RestartBanner, RegistrySection, RegistryField, namespaceOf, labelFor } from "@/features/settings/registry";
import type { RegistrySectionField } from "@/features/settings/registry";

// Operational namespaces rendered as schema-driven sections.
const OPERATIONAL_NAMESPACES = ["http", "agents", "auth", "jobs", "observability", "storage"] as const;

// Compact "Admin" pill reused across admin-gated sections.
function AdminBadge() {
  return (
    <span className="inline-flex items-center gap-1 rounded-xs border border-accent/20 bg-accent/5 px-1.5 py-0.5 text-[9px] font-mono uppercase tracking-wider text-accent">
      <ShieldCheck className="h-2.5 w-2.5" aria-hidden />
      Admin
    </span>
  );
}

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
  const hasAdmin = !!(onManageUsers || (retentionSettings && onRetentionChange) || onRestart);

  return (
    <div className="flex flex-col">
      <PageHeader
        title="Settings"
        subtitle="Configure your control plane"
        trailing={
          hasAdmin ? (
            <span className="inline-flex items-center gap-1.5 rounded-xs border border-accent/30 bg-accent/10 px-2 py-1 text-[10px] font-mono uppercase tracking-wider text-accent">
              <ShieldCheck className="h-3 w-3" aria-hidden />
              Admin
            </span>
          ) : (
            <span className="inline-flex items-center gap-1.5 rounded-xs border border-border bg-bg-card px-2 py-1 text-[10px] font-mono uppercase tracking-wider text-fg-muted">
              User
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
              {Object.keys(registry.draft).length}{" "}
              unsaved {Object.keys(registry.draft).length === 1 ? "change" : "changes"}
            </span>
            <div className="flex gap-2">
              <Button
                variant="ghost"
                size="sm"
                onClick={registry.onCancelDraft}
                disabled={registry.isSaving}
              >
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={registry.onSave}
                disabled={registry.isSaving}
              >
                <Save className="h-3.5 w-3.5 mr-1" aria-hidden />
                {registry.isSaving ? "Saving…" : "Save"}
              </Button>
            </div>
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
              title="Panel"
              description="Endpoints the control plane and agents advertise publicly."
            >
              <SettingsRow
                label="HTTP Public URL"
                description="Public-facing URL for this control plane"
              >
                <Input
                  className="w-64"
                  value={panelSettings.httpPublicUrl}
                  placeholder="https://panvex.example.com"
                  onChange={(e) =>
                    onPanelSettingsChange?.({
                      ...panelSettings,
                      httpPublicUrl: e.target.value,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow label="gRPC Endpoint" description="Agent connection endpoint">
                <Input
                  className="w-64"
                  value={panelSettings.grpcPublicEndpoint}
                  placeholder="panvex.example.com:443"
                  onChange={(e) =>
                    onPanelSettingsChange?.({
                      ...panelSettings,
                      grpcPublicEndpoint: e.target.value,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow
                label="Minimum password length"
                description="Operators creating or rotating passwords must meet this floor (8–128). Existing accounts are not invalidated."
              >
                <Input
                  type="number"
                  className="w-24"
                  min={8}
                  max={128}
                  step={1}
                  value={panelSettings.passwordMinLength}
                  onChange={(e) =>
                    onPanelSettingsChange?.({
                      ...panelSettings,
                      passwordMinLength: Number(e.target.value) || 8,
                    })
                  }
                  aria-label="Minimum password length"
                />
              </SettingsRow>
            </PageSection>

            <PageSection
              icon={Palette}
              title="Appearance"
              description="How the dashboard looks and feels for your account."
            >
              <SettingsRow label="Theme">
                <Select
                  className="w-36"
                  value={appearanceSettings.theme}
                  options={[
                    { value: "system", label: "System" },
                    { value: "light", label: "Light" },
                    { value: "dark", label: "Dark" },
                  ]}
                  onChange={(v) =>
                    onAppearanceChange?.({
                      ...appearanceSettings,
                      theme: v as typeof appearanceSettings.theme,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow label="Density">
                <Select
                  className="w-36"
                  value={appearanceSettings.density}
                  options={[
                    { value: "comfortable", label: "Comfortable" },
                    { value: "compact", label: "Compact" },
                  ]}
                  onChange={(v) =>
                    onAppearanceChange?.({
                      ...appearanceSettings,
                      density: v as typeof appearanceSettings.density,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow label="Help Mode">
                <Select
                  className="w-36"
                  value={appearanceSettings.helpMode}
                  options={[
                    { value: "off", label: "Off" },
                    { value: "basic", label: "Basic" },
                    { value: "full", label: "Full" },
                  ]}
                  onChange={(v) =>
                    onAppearanceChange?.({
                      ...appearanceSettings,
                      helpMode: v as typeof appearanceSettings.helpMode,
                    })
                  }
                />
              </SettingsRow>
              <SettingsRow label="Swipe Navigation" description="Swipe between pages on mobile">
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
                title="User Management"
                description="Create, edit, and manage user accounts for this panel."
                badge={<AdminBadge />}
              >
                <SettingsRow
                  label="Accounts"
                  description="Go to the user list to add admins, rotate passwords, or revoke access."
                >
                  <Button size="sm" onClick={onManageUsers}>
                    Manage Users
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
                title="System"
                description="Destructive operations on the control plane itself."
                badge={<AdminBadge />}
                tone="danger"
              >
                <SettingsRow
                  label="Restart Control Plane"
                  description="Gracefully restart the control-plane process. Active WebSocket and gRPC connections will briefly drop."
                >
                  <Button variant="danger" size="sm" onClick={onRestart}>
                    Restart
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

// ─── SystemInfo ───────────────────────────────────────────────────────────────

function SystemInfoSection({ registry }: Readonly<{ registry: SettingsRegistryProps }>) {
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
        title="System info"
        description="Bootstrap settings — locked by environment or config file."
      >
        {Array.from(byNamespace.entries()).map(([ns, fields]) => {
          const label = labelFor(ns);
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

const RETENTION_FIELDS: {
  key: keyof NonNullable<SettingsPageProps["retentionSettings"]>;
  label: string;
  description: string;
}[] = [
  {
    key: "ts_raw_seconds",
    label: "Raw Metrics",
    description: "Server load and DC health raw data points",
  },
  { key: "ts_hourly_seconds", label: "Hourly Rollups", description: "Aggregated hourly metrics" },
  { key: "ts_dc_seconds", label: "DC Health", description: "Per-DC coverage and RTT history" },
  {
    key: "ip_history_seconds",
    label: "Client IP History",
    description: "Client IP address records",
  },
  {
    key: "event_history_seconds",
    label: "Runtime Events",
    description: "Telemt runtime event log",
  },
];

const UNITS = [
  { value: "seconds", label: "Seconds" },
  { value: "minutes", label: "Minutes" },
  { value: "hours", label: "Hours" },
  { value: "days", label: "Days" },
];

function RetentionSection({
  settings,
  onChange,
  saving,
}: Readonly<{
  settings: NonNullable<SettingsPageProps["retentionSettings"]>;
  onChange: (s: Readonly<NonNullable<SettingsPageProps["retentionSettings"]>>) => void;
  saving?: boolean | undefined;
}>) {
  const [draft, setDraft] = useState(settings);
  const isDirty = JSON.stringify(draft) !== JSON.stringify(settings);

  function updateField(key: keyof typeof draft, value: number, unit: string) {
    setDraft((prev) => ({ ...prev, [key]: displayToSeconds(value, unit) }));
  }

  return (
    <PageSection
      icon={Database}
      title="Data Retention"
      description="How long the panel keeps timeseries and history records before pruning."
      badge={<AdminBadge />}
    >
      {RETENTION_FIELDS.map(({ key, label, description }) => {
        const display = secondsToDisplay(draft[key]);
        return (
          <SettingsRow key={key} label={label} description={description}>
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
                options={UNITS}
              />
            </div>
          </SettingsRow>
        );
      })}
      {isDirty && (
        <div className="flex items-center justify-between px-4 py-3 bg-accent/5">
          <span className="text-xs font-mono text-fg-muted">
            Unsaved changes · retention windows will apply on next prune cycle.
          </span>
          <div className="flex gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setDraft(settings)}
              disabled={saving}
            >
              Cancel
            </Button>
            <Button size="sm" onClick={() => onChange(draft)} disabled={saving}>
              {saving ? "Saving…" : "Save Retention Settings"}
            </Button>
          </div>
        </div>
      )}
    </PageSection>
  );
}
