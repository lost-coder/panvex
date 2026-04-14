import { useState } from "react";
import { SettingsGroup, SettingsRow, Button, Input } from "@panvex/ui";
import { useUpdates } from "@/hooks/useUpdates";
import type { UpdateSettings } from "@/lib/api";

function formatTimestamp(unix: number): string {
  if (!unix) return "Never";
  return new Date(unix * 1000).toLocaleString();
}

export function UpdatesSettingsSection() {
  const { query, saveSettings, checkNow, updatePanel } = useUpdates();
  const data = query.data;

  const [draft, setDraft] = useState<Partial<UpdateSettings>>({});

  if (!data) return null;

  const settings: UpdateSettings = { ...data.settings, ...draft };
  const state = data.state;
  const isDirty = Object.keys(draft).length > 0;

  const hasNewerPanel =
    state.latest_panel_version &&
    state.latest_panel_version !== data.current_version;

  function applyDraft(partial: Partial<UpdateSettings>) {
    setDraft((prev) => ({ ...prev, ...partial }));
  }

  function handleSave() {
    saveSettings.mutate(settings, { onSuccess: () => setDraft({}) });
  }

  function handleCancel() {
    setDraft({});
  }

  return (
    <SettingsGroup title="Updates">
      {/* Current version / panel update */}
      <SettingsRow
        label="Panel Version"
        description={
          hasNewerPanel
            ? `Update available: ${state.latest_panel_version}`
            : "Up to date"
        }
      >
        <div className="flex items-center gap-2">
          <span className="text-sm text-fg-muted font-mono">{data.current_version || "—"}</span>
          {hasNewerPanel && (
            <Button
              size="sm"
              disabled={updatePanel.isPending}
              onClick={() => updatePanel.mutate(state.latest_panel_version)}
            >
              {updatePanel.isPending ? "Updating…" : "Update Panel"}
            </Button>
          )}
        </div>
      </SettingsRow>

      {/* Latest agent version */}
      <SettingsRow
        label="Latest Agent Version"
        description="Newest agent release available for deployment"
      >
        <span className="text-sm text-fg-muted font-mono">
          {state.latest_agent_version || "—"}
        </span>
      </SettingsRow>

      {/* Last checked / check now */}
      <SettingsRow
        label="Last Checked"
        description={formatTimestamp(state.last_checked_at)}
      >
        <Button
          size="sm"
          variant="ghost"
          disabled={checkNow.isPending}
          onClick={() => checkNow.mutate()}
        >
          {checkNow.isPending ? "Checking…" : "Check Now"}
        </Button>
      </SettingsRow>

      {/* Check interval */}
      <SettingsRow
        label="Check Interval"
        description="How often to check for new releases (hours)"
      >
        <Input
          type="number"
          min={1}
          className="w-20"
          value={settings.check_interval_hours}
          onChange={(e) =>
            applyDraft({ check_interval_hours: Number(e.target.value) || 1 })
          }
        />
      </SettingsRow>

      {/* Auto-update panel */}
      <SettingsRow
        label="Auto-Update Panel"
        description="Automatically apply panel updates when available"
      >
        <input
          type="checkbox"
          className="h-4 w-4 accent-[var(--color-accent)] cursor-pointer"
          checked={settings.auto_update_panel}
          onChange={(e) => applyDraft({ auto_update_panel: e.target.checked })}
        />
      </SettingsRow>

      {/* Auto-update agents */}
      <SettingsRow
        label="Auto-Update Agents"
        description="Automatically push agent updates across the fleet"
      >
        <input
          type="checkbox"
          className="h-4 w-4 accent-[var(--color-accent)] cursor-pointer"
          checked={settings.auto_update_agents}
          onChange={(e) => applyDraft({ auto_update_agents: e.target.checked })}
        />
      </SettingsRow>

      {/* Save / cancel */}
      {isDirty && (
        <div className="flex justify-end px-4 py-3">
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={handleCancel}>
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={saveSettings.isPending}
              onClick={handleSave}
            >
              {saveSettings.isPending ? "Saving…" : "Save Update Settings"}
            </Button>
          </div>
        </div>
      )}
    </SettingsGroup>
  );
}
