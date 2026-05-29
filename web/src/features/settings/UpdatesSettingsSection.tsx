import { useState } from "react";
import { useTranslation } from "react-i18next";
import { PageSection, SettingsRow, Button, Input } from "@/ui";
import { Download, RefreshCw } from "lucide-react";
import { useUpdates } from "@/shared/hooks/useUpdates";
import type { UpdateSettings } from "@/shared/api/api";

export function UpdatesSettingsSection() {
  const { t } = useTranslation("settings");
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
    <PageSection
      icon={Download}
      title={t("updates.title")}
      description={t("updates.description")}
    >
      {/* Update available banner */}
      {hasNewerPanel && (
        <div className="mx-4 mt-3 rounded-lg border border-accent/30 bg-accent/5 px-4 py-3 flex items-center justify-between">
          <div>
            <p className="text-sm font-medium text-fg">
              {t("updates.panelUpdateAvailable")}
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
            {updatePanel.isPending
              ? t("updates.updating")
              : t("updates.updateButton", { version: state.latest_panel_version })}
          </Button>
        </div>
      )}

      {/* Last update-check error (e.g. GitHub rate limit). */}
      {state.last_check_error && (
        <div className="mx-4 mt-3 rounded-lg border border-red-500/30 bg-red-500/5 px-4 py-3">
          <p className="text-sm font-medium text-red-400">
            {t("updates.checkError")}
          </p>
          <p className="mt-0.5 text-xs text-fg-muted break-words">
            {state.last_check_error}
          </p>
        </div>
      )}

      {/* Versions: compact 2-column layout */}
      <SettingsRow label={t("updates.versionsLabel")}>
        <div className="flex items-center gap-6 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-fg-muted">{t("updates.panel")}</span>
            <span className="font-mono text-fg">{data.current_version || "—"}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-fg-muted">{t("updates.agents")}</span>
            <span className="font-mono text-fg">{state.latest_agent_version || "—"}</span>
          </div>
          <button
            className="inline-flex items-center justify-center rounded-md p-1 text-fg-muted hover:text-fg hover:bg-surface-hover transition-colors disabled:opacity-50"
            disabled={checkNow.isPending}
            onClick={() => checkNow.mutate()}
            title={t("updates.checkNowTitle")}
          >
            <RefreshCw className={`h-4 w-4 ${checkNow.isPending ? "animate-spin" : ""}`} />
          </button>
        </div>
      </SettingsRow>

      {/* Auto-update toggles: disabled until the auto-apply worker lands. */}
      <SettingsRow label={t("updates.autoUpdateLabel")}>
        <div className="flex items-center gap-5 text-sm">
          <label className="flex items-center gap-1.5 cursor-not-allowed opacity-50">
            <input
              type="checkbox"
              className="h-4 w-4 accent-[var(--color-accent)]"
              checked={settings.auto_update_panel}
              disabled
              readOnly
            />
            <span className="text-fg-muted">{t("updates.panel")}</span>
          </label>
          <label className="flex items-center gap-1.5 cursor-not-allowed opacity-50">
            <input
              type="checkbox"
              className="h-4 w-4 accent-[var(--color-accent)]"
              checked={settings.auto_update_agents}
              disabled
              readOnly
            />
            <span className="text-fg-muted">{t("updates.agents")}</span>
          </label>
          <span className="rounded-full bg-surface-hover px-2 py-0.5 text-xs text-fg-muted">
            {t("updates.comingSoon")}
          </span>
        </div>
      </SettingsRow>

      {/* Check interval: inline input */}
      <SettingsRow label={t("updates.checkIntervalLabel")}>
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
          <span className="text-fg-muted">{t("updates.hours")}</span>
        </div>
      </SettingsRow>

      {/* Save / cancel */}
      {isDirty && (
        <div className="flex justify-end px-4 py-3">
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={handleCancel}>
              {t("updates.cancel")}
            </Button>
            <Button
              size="sm"
              disabled={saveSettings.isPending}
              onClick={handleSave}
            >
              {saveSettings.isPending ? t("updates.saving") : t("updates.save")}
            </Button>
          </div>
        </div>
      )}
    </PageSection>
  );
}
