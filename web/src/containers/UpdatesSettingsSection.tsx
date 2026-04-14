import { useState } from "react";
import { SettingsGroup, SettingsRow, Button, Input } from "@lost-coder/panvex-ui";
import { RefreshCw } from "lucide-react";
import { useUpdates } from "@/hooks/useUpdates";
import type { UpdateSettings } from "@/lib/api";

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
      {/* Update available banner */}
      {hasNewerPanel && (
        <div className="mx-4 mt-3 rounded-lg border border-accent/30 bg-accent/5 px-4 py-3 flex items-center justify-between">
          <div>
            <p className="text-sm font-medium text-fg">
              Panel update available
            </p>
            <p className="text-xs text-fg-muted">
              {data.current_version} → {state.latest_panel_version}
            </p>
          </div>
          <Button
            size="sm"
            disabled={updatePanel.isPending}
            onClick={() => updatePanel.mutate(state.latest_panel_version)}
          >
            {updatePanel.isPending ? "Updating…" : `Update to ${state.latest_panel_version}`}
          </Button>
        </div>
      )}

      {/* Versions: compact 2-column layout */}
      <SettingsRow label="Versions">
        <div className="flex items-center gap-6 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-fg-muted">Panel</span>
            <span className="font-mono text-fg">{data.current_version || "—"}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-fg-muted">Agents</span>
            <span className="font-mono text-fg">{state.latest_agent_version || "—"}</span>
          </div>
          <button
            className="inline-flex items-center justify-center rounded-md p-1 text-fg-muted hover:text-fg hover:bg-surface-hover transition-colors disabled:opacity-50"
            disabled={checkNow.isPending}
            onClick={() => checkNow.mutate()}
            title="Check for updates"
          >
            <RefreshCw className={`h-4 w-4 ${checkNow.isPending ? "animate-spin" : ""}`} />
          </button>
        </div>
      </SettingsRow>

      {/* Auto-update toggles: single row with two inline toggles */}
      <SettingsRow label="Auto-Update">
        <div className="flex items-center gap-5 text-sm">
          <label className="flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              className="h-4 w-4 accent-[var(--color-accent)] cursor-pointer"
              checked={settings.auto_update_panel}
              onChange={(e) => applyDraft({ auto_update_panel: e.target.checked })}
            />
            <span className="text-fg-muted">Panel</span>
          </label>
          <label className="flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              className="h-4 w-4 accent-[var(--color-accent)] cursor-pointer"
              checked={settings.auto_update_agents}
              onChange={(e) => applyDraft({ auto_update_agents: e.target.checked })}
            />
            <span className="text-fg-muted">Agents</span>
          </label>
        </div>
      </SettingsRow>

      {/* Check interval: inline input */}
      <SettingsRow label="Check Interval">
        <div className="flex items-center gap-2 text-sm">
          <Input
            type="number"
            min={1}
            className="w-16"
            value={settings.check_interval_hours}
            onChange={(e) =>
              applyDraft({ check_interval_hours: Number(e.target.value) || 1 })
            }
          />
          <span className="text-fg-muted">hours</span>
        </div>
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
              {saveSettings.isPending ? "Saving…" : "Save"}
            </Button>
          </div>
        </div>
      )}
    </SettingsGroup>
  );
}
