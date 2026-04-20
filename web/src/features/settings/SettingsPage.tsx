import { useState } from "react";
import { PageHeader } from "@/ui/layout/PageHeader";
import { SettingsGroup } from "@/ui/components/SettingsGroup";
import { SettingsRow } from "@/ui/components/SettingsRow";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { Button } from "@/ui/base/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/ui/base/tabs";
import { secondsToDisplay, displayToSeconds } from "@/shared/lib/pages-shared";
import type { SettingsPageProps } from "@/shared/api/types-pages/pages";

// Whether the Administration tab should be visible (admin-only sections present)
function hasAdminContent(props: SettingsPageProps) {
  return !!(
    props.onManageUsers ||
    (props.retentionSettings && props.onRetentionChange) ||
    props.onRestart
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
  children,
}: SettingsPageProps) {
  const showAdmin = hasAdminContent({
    panelSettings,
    appearanceSettings,
    onManageUsers,
    onRestart,
    retentionSettings,
    onRetentionChange,
  });

  return (
    <div className="flex flex-col">
      <PageHeader title="Settings" subtitle="Configure your control plane" />

      <div className="px-4 md:px-8 pb-8">
        <Tabs defaultValue="general">
          <TabsList className="w-full overflow-x-auto">
            <TabsTrigger value="general">General</TabsTrigger>
            {showAdmin && <TabsTrigger value="updates">Updates</TabsTrigger>}
            {showAdmin && <TabsTrigger value="administration">Administration</TabsTrigger>}
          </TabsList>

          {/* General tab */}
          <TabsContent value="general">
            <div className="flex flex-col gap-6">
              {/* Panel */}
              <SettingsGroup title="Panel">
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
              </SettingsGroup>

              {/* Appearance */}
              <SettingsGroup title="Appearance">
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
              </SettingsGroup>
            </div>
          </TabsContent>

          {/* Updates tab — content injected by parent container (admin only) */}
          {showAdmin && <TabsContent value="updates">{children}</TabsContent>}

          {/* Administration tab — only rendered for admins */}
          {showAdmin && (
            <TabsContent value="administration">
              <div className="flex flex-col gap-6">
                {/* User Management */}
                {onManageUsers && (
                  <SettingsGroup title="User Management">
                    <SettingsRow
                      label="User Management"
                      description="Create, edit, and manage user accounts"
                    >
                      <Button size="sm" onClick={onManageUsers}>
                        Manage Users
                      </Button>
                    </SettingsRow>
                  </SettingsGroup>
                )}

                {/* Data Retention */}
                {retentionSettings && onRetentionChange && (
                  <RetentionSection settings={retentionSettings} onChange={onRetentionChange} />
                )}

                {/* System */}
                <SettingsGroup title="System">
                  <SettingsRow
                    label="Restart Control Plane"
                    description="Gracefully restart the control-plane process"
                  >
                    <Button variant="danger" size="sm" onClick={onRestart}>
                      Restart
                    </Button>
                  </SettingsRow>
                </SettingsGroup>
              </div>
            </TabsContent>
          )}
        </Tabs>
      </div>
    </div>
  );
}

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
}: {
  settings: NonNullable<SettingsPageProps["retentionSettings"]>;
  onChange: (s: NonNullable<SettingsPageProps["retentionSettings"]>) => void;
}) {
  const [draft, setDraft] = useState(settings);
  const isDirty = JSON.stringify(draft) !== JSON.stringify(settings);

  function updateField(key: keyof typeof draft, value: number, unit: string) {
    setDraft((prev) => ({ ...prev, [key]: displayToSeconds(value, unit) }));
  }

  return (
    <SettingsGroup title="Data Retention">
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
        <div className="flex justify-end px-4 py-3">
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={() => setDraft(settings)}>
              Cancel
            </Button>
            <Button size="sm" onClick={() => onChange(draft)}>
              Save Retention Settings
            </Button>
          </div>
        </div>
      )}
    </SettingsGroup>
  );
}
