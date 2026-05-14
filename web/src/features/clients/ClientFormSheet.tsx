import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { FormField } from "@/ui/base/form-field";
import { cn } from "@/ui/lib/cn";
import type { ClientFormSheetProps } from "@/shared/api/types-pages/pages";

// Mirrors Telemt's username constraint (and the panel's clientNameRegex
// in internal/controlplane/server/clients_flow.go). Catching it client-side
// avoids round-tripping just to surface a guaranteed 400.
const CLIENT_NAME_REGEX = /^[A-Za-z0-9_.-]{1,64}$/;

export function ClientFormSheet({
  mode,
  data,
  onChange,
  onSubmit,
  onCancel,
  loading,
  error,
  fleetGroups,
  agents,
}: Readonly<ClientFormSheetProps>) {
  const { t } = useTranslation("clients");
  const nameRule = t("form.nameRule");
  const [showLimits, setShowLimits] = useState(
    data.maxTcpConns > 0 || data.maxUniqueIps > 0 || data.dataQuotaBytes > 0,
  );

  const trimmedName = data.name.trim();
  const nameInvalid = trimmedName.length > 0 && !CLIENT_NAME_REGEX.test(trimmedName);

  function update<K extends keyof typeof data>(key: K, value: (typeof data)[K]) {
    onChange({ ...data, [key]: value });
  }

  // Toggling a fleet group is a set-membership flip. The backend accepts
  // fleet_group_ids and agent_ids as independent lists so we keep them
  // decoupled here — an operator can assign by group AND pin a few extra
  // agents if they want.
  function toggleFleetGroup(id: string) {
    const next = data.fleetGroupIds.includes(id)
      ? data.fleetGroupIds.filter((x) => x !== id)
      : [...data.fleetGroupIds, id];
    update("fleetGroupIds", next);
  }

  function toggleAgent(id: string) {
    const next = data.agentIds.includes(id)
      ? data.agentIds.filter((x) => x !== id)
      : [...data.agentIds, id];
    update("agentIds", next);
  }

  // Group agents by fleet-group so the multi-select shows a logical
  // hierarchy matching the backend model. Agents without a fleet group
  // land in a "Default" bucket.
  const agentsByGroup = useMemo(() => {
    const map = new Map<string, typeof agents>();
    for (const a of agents ?? []) {
      const key = a.fleetGroupId || "default";
      const bucket = map.get(key) ?? [];
      bucket.push(a);
      map.set(key, bucket);
    }
    return map;
  }, [agents]);

  const hasDeploymentTargets =
    data.fleetGroupIds.length > 0 || data.agentIds.length > 0;

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-title">{mode === "create" ? t("form.createTitle") : t("form.editTitle")}</h3>
        <p className="text-sm text-fg-muted mt-0.5">
          {mode === "create" ? t("form.createSubtitle") : t("form.editSubtitle")}
        </p>
      </div>

      <FormField
        label={t("form.nameLabel")}
        variant="uppercase"
        required
        description={nameRule}
        {...(nameInvalid ? { error: nameRule } : {})}
      >
        <Input
          value={data.name}
          onChange={(e) => update("name", e.target.value)}
          placeholder={t("form.namePlaceholder")}
          disabled={loading}
          aria-invalid={nameInvalid || undefined}
          maxLength={64}
        />
      </FormField>

      <FormField
        label={t("form.adTagLabel")}
        variant="uppercase"
        description={
          data.userAdTagAuto
            ? t("form.adTagAutoDescription")
            : t("form.adTagManualDescription")
        }
      >
        <div className="flex flex-col gap-2">
          <label className="flex items-center gap-2 text-xs text-fg-muted">
            <input
              type="checkbox"
              className="accent-accent size-3.5"
              checked={data.userAdTagAuto}
              disabled={loading}
              onChange={(e) => {
                const next = e.target.checked;
                onChange({
                  ...data,
                  userAdTagAuto: next,
                  // When the operator ticks the box the typed value
                  // would be overwritten by the server anyway, so wipe
                  // the input to avoid confusion.
                  userAdTag: next ? "" : data.userAdTag,
                });
              }}
            />
            <span>{t("form.adTagAutoCheckbox")}</span>
          </label>
          <Input
            value={data.userAdTag}
            onChange={(e) => update("userAdTag", e.target.value)}
            placeholder={
              data.userAdTagAuto
                ? t("form.adTagAutoPlaceholder")
                : t("form.adTagManualPlaceholder")
            }
            disabled={loading || data.userAdTagAuto}
            className="font-mono text-xs"
          />
        </div>
      </FormField>

      <FormField label={t("form.expirationLabel")} variant="uppercase">
        <div className="flex gap-2">
          <Input
            type="date"
            value={data.expirationRfc3339 ? data.expirationRfc3339.slice(0, 10) : ""}
            onChange={(e) =>
              update(
                "expirationRfc3339",
                // P2-FE-04 / M-C9: emit the picked calendar day as RFC3339
                // anchored at noon UTC. `new Date("YYYY-MM-DD")` treats the
                // string as UTC midnight — which, when re-rendered in any
                // UTC-offset zone or compared against local time, can shift
                // to the previous or next calendar day. Anchoring at 12:00Z
                // keeps the ISO string's date component equal to the picked
                // day for every timezone from UTC-11 through UTC+11.
                e.target.value ? `${e.target.value}T12:00:00.000Z` : "",
              )
            }
            className="flex-1"
            disabled={loading}
          />
          <Button
            variant={data.expirationRfc3339 ? "ghost" : "default"}
            size="sm"
            onClick={() => update("expirationRfc3339", "")}
          >
            {t("form.expirationNever")}
          </Button>
        </div>
      </FormField>

      {(fleetGroups?.length || agents?.length) && (
        <div className="flex flex-col gap-3 border-t border-border pt-3 mt-1">
          <div className="flex items-center justify-between">
            <span className="text-xs uppercase tracking-wide text-fg-muted">
              {t("form.deploymentTargets")}
            </span>
            {!hasDeploymentTargets && (
              <span className="text-[11px] text-status-warn">{t("form.deploymentTargetsHint")}</span>
            )}
          </div>

          {fleetGroups && fleetGroups.length > 0 && (
            <FormField label={t("form.fleetGroupsLabel")} variant="uppercase">
              <div className="flex flex-wrap gap-1.5">
                {fleetGroups.map((g) => {
                  const active = data.fleetGroupIds.includes(g.id);
                  const label = g.label ?? g.name ?? g.id;
                  const count = g.agentCount ?? g.nodeCount;
                  return (
                    <button
                      key={g.id}
                      type="button"
                      disabled={loading}
                      onClick={() => toggleFleetGroup(g.id)}
                      aria-pressed={active}
                      className={cn(
                        "px-2.5 py-1 rounded-xs text-xs font-mono border transition-colors",
                        active
                          ? "bg-accent text-white border-accent"
                          : "bg-bg-card text-fg-muted border-border-hi hover:text-fg hover:border-accent/60",
                        loading && "opacity-50 cursor-not-allowed",
                      )}
                    >
                      {label}
                      {typeof count === "number" && (
                        <span className="ml-1 opacity-60 tabular-nums">·{count}</span>
                      )}
                    </button>
                  );
                })}
              </div>
            </FormField>
          )}

          {agents && agents.length > 0 && (
            <FormField
              label={t("form.pinnedAgentsLabel")}
              variant="uppercase"
              description={t("form.pinnedAgentsDescription")}
            >
              <div className="max-h-40 overflow-y-auto rounded-xs border border-border-hi bg-bg-card divide-y divide-border/60">
                {Array.from(agentsByGroup.entries()).map(([groupId, list]) => (
                  <div key={groupId} className="flex flex-col">
                    <div className="px-2 py-1 text-[10px] uppercase tracking-wide text-fg-muted bg-bg-muted/40">
                      {groupId}
                    </div>
                    {(list ?? []).map((a) => {
                      const active = data.agentIds.includes(a.id);
                      return (
                        <label
                          key={a.id}
                          className={cn(
                            "flex items-center gap-2 px-2 py-1.5 text-xs cursor-pointer hover:bg-bg-muted/40",
                            loading && "opacity-50 cursor-not-allowed",
                          )}
                        >
                          <input
                            type="checkbox"
                            className="accent-accent size-3.5"
                            checked={active}
                            disabled={loading}
                            onChange={() => toggleAgent(a.id)}
                          />
                          <span className="font-mono text-fg truncate">{a.nodeName || a.id}</span>
                          {a.online === false && (
                            <span className="ml-auto text-[10px] text-status-warn">{t("form.agentOffline")}</span>
                          )}
                        </label>
                      );
                    })}
                  </div>
                ))}
              </div>
            </FormField>
          )}
        </div>
      )}

      <button
        type="button"
        onClick={() => setShowLimits(!showLimits)}
        aria-expanded={showLimits}
        aria-controls="client-form-limits-section"
        className="text-xs text-fg-muted hover:text-fg text-left border-t border-border pt-3 mt-1"
      >
        {showLimits ? t("form.limitsToggleExpanded") : t("form.limitsToggleCollapsed")}
      </button>
      {showLimits && (
        <div id="client-form-limits-section" className="grid grid-cols-3 gap-3">
          <FormField label={t("form.maxTcpConnsLabel")} variant="compact">
            <Input
              type="number"
              value={data.maxTcpConns}
              onChange={(e) => update("maxTcpConns", Number(e.target.value))}
              placeholder={t("form.unlimitedPlaceholder")}
              className="font-mono text-xs"
              disabled={loading}
            />
          </FormField>
          <FormField label={t("form.maxUniqueIpsLabel")} variant="compact">
            <Input
              type="number"
              value={data.maxUniqueIps}
              onChange={(e) => update("maxUniqueIps", Number(e.target.value))}
              placeholder={t("form.unlimitedPlaceholder")}
              className="font-mono text-xs"
              disabled={loading}
            />
          </FormField>
          <FormField label={t("form.dataQuotaLabel")} variant="compact">
            <Input
              type="number"
              value={data.dataQuotaBytes}
              onChange={(e) => update("dataQuotaBytes", Number(e.target.value))}
              placeholder={t("form.unlimitedPlaceholder")}
              className="font-mono text-xs"
              disabled={loading}
            />
          </FormField>
        </div>
      )}

      {error && <div className="text-xs text-status-error">{error}</div>}

      <div className="flex gap-2 justify-end mt-2">
        <Button variant="ghost" onClick={onCancel} disabled={loading}>
          {t("form.cancel")}
        </Button>
        <Button
          onClick={onSubmit}
          disabled={
            loading ||
            !trimmedName ||
            nameInvalid ||
            // Selectors are only rendered when the container supplied options.
            // When they are rendered, at least one target must be picked —
            // the backend otherwise rejects with errClientTargetsRequired.
            (!!(fleetGroups?.length || agents?.length) && !hasDeploymentTargets)
          }
        >
          {(() => {
            if (loading) return t("form.saving");
            if (mode === "create") return t("form.create");
            return t("form.save");
          })()}
        </Button>
      </div>
    </div>
  );
}
