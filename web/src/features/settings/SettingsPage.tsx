// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: single-scroll, multi-section page. No rail nav — with only
// 2–6 sections scrolling beats a table-of-contents.
import { useState } from "react";
import {
  Palette,
  Download,
  Users as UsersIcon,
  Database,
  Power,
  Server as ServerIcon,
  ShieldCheck,
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
import type { SettingsPageProps } from "@/shared/api/types-pages/pages";

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

      <div className="px-4 md:px-8 pb-8">
        {/* 2-col grid on desktop keeps both halves of the viewport busy:
            short sections (System, Users) sit next to longer ones (Retention,
            Appearance) instead of leaving empty half-screen real estate. */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-8 items-start">
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

            {hasAdmin && (
              <PageSection
                icon={Download}
                title="Updates"
                description="Panel + agent version management."
                badge={<AdminBadge />}
              >
                {children}
              </PageSection>
            )}

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
        </div>
      </div>
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
