// P5-T6: the Server-detail "Config" tab.
//
// Fetches the agent's config (override / effective / observed + drift) via
// useAgentConfig, seeds a local editor map from the OVERRIDE, and lets the
// operator edit the curated CONFIG_FIELDS. Save persists the override
// (PUT, nested sections); Apply pushes the override down to the running
// Telemt process. The drift header surfaces whether the observed config has
// diverged from the effective target, listing the diverging fields.
//
// The editor is fully controlled, so this tab owns the dotted-path → value
// map. We track which paths the user touched (changedPaths) against the
// initial flatten so the Apply gate only lights up — and the restart-warning
// only fires — for genuinely-changed fields, surviving a Save→refetch round
// trip via the data-keyed reset effect.

import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Badge, Button, Spinner } from "@/ui";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { useToast } from "@/app/providers/ToastProvider";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import {
  useAgentConfig,
  useApplyAgentConfig,
  usePutAgentConfig,
} from "@/features/servers/config/configHooks";
import { ConfigSectionEditor } from "@/features/servers/config/ConfigSectionEditor";
import { DriftBadge } from "@/features/servers/config/DriftBadge";
import { ApplyConfigButton } from "@/features/servers/config/ApplyConfigButton";
import {
  flattenSections,
  unflattenPaths,
} from "@/features/servers/config/sections";

// Compute the set of dotted paths whose current value differs from the
// initial (override-seeded) flatten. Used both for the Apply gate and the
// restart-warning decision inside ApplyConfigButton.
function diffPaths(
  initial: Record<string, unknown>,
  current: Record<string, unknown>,
): string[] {
  const paths = new Set([...Object.keys(initial), ...Object.keys(current)]);
  const out: string[] = [];
  for (const p of paths) {
    if (JSON.stringify(initial[p]) !== JSON.stringify(current[p])) out.push(p);
  }
  return out;
}

export function ConfigTab({
  server,
}: Readonly<{ server: ServerDetailPageProps["server"] }>) {
  const { t } = useTranslation("servers");
  const toast = useToast();

  const agentId = server.id;
  const { data, isLoading, isError } = useAgentConfig(agentId);
  const putMutation = usePutAgentConfig(agentId);
  const applyMutation = useApplyAgentConfig(agentId);

  // Editor state — seeded from the OVERRIDE. We keep the initial flatten so
  // the changed-path diff is stable across re-renders.
  const initialValues = useMemo(
    () => flattenSections(data?.override ?? {}),
    [data?.override],
  );
  // U-13: effective config powers placeholder hints so an un-overridden
  // (empty) field reads as "inherits this value", not "blank/wipe".
  const effectiveValues = useMemo(
    () => flattenSections(data?.effective ?? {}),
    [data?.effective],
  );
  const [values, setValues] = useState<Record<string, unknown>>(initialValues);

  // Paths the operator has edited but not yet saved — drives the dirty
  // state that blocks Apply (you save the override before pushing it).
  const changedPaths = useMemo(
    () => diffPaths(initialValues, values),
    [initialValues, values],
  );

  // 3.12: re-seed the editor from a fresh override on initial load, on a
  // genuine identity change (switched to a different server), or on a
  // post-Save/post-Apply refetch where the operator has no unsaved edits.
  // Previously this ran on every `initialValues` change unconditionally —
  // a background refetch (including the WS seq-gap full-cache
  // invalidation) while the operator was mid-edit would silently wipe
  // their unsaved changes.
  //
  // `lastSeededRef` snapshots the initialValues the editor was last reset
  // to. Dirtiness is `values` vs. THAT snapshot, not vs. the just-arrived
  // `initialValues` — on the render where `agentId` changes, `values` still
  // holds the previous server's draft, so diffing it against the brand-new
  // `initialValues` would spuriously read as "dirty" and block the very
  // re-seed an id-change is supposed to force.
  const lastSeededRef = useRef(initialValues);
  const lastAgentIdRef = useRef(agentId);
  useEffect(() => {
    const idChanged = lastAgentIdRef.current !== agentId;
    lastAgentIdRef.current = agentId;
    if (!idChanged && diffPaths(lastSeededRef.current, values).length > 0) {
      // Refetch landed while the operator has unsaved edits on the SAME
      // server — keep their draft.
      return;
    }
    lastSeededRef.current = initialValues;
    setValues(initialValues);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, initialValues]);

  // What Apply will push: the persisted override's own paths. Feeding these
  // to ApplyConfigButton lets it decide whether a restart-warning confirm is
  // needed (e.g. a restart-only field like censorship.tls_domain is set),
  // independent of the unsaved-edit diff above.
  const overridePaths = useMemo(
    () => Object.keys(initialValues),
    [initialValues],
  );

  if (isLoading) {
    return (
      <div
        className="flex items-center justify-center gap-2 px-4 py-8 text-xs text-fg-muted"
        aria-busy
        aria-live="polite"
      >
        <Spinner />
        {t("loading.tab")}
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="py-8 text-center text-sm text-status-error">
        {t("error.requestFailed")}
      </div>
    );
  }

  const drift = data.drift;

  function handleSave() {
    putMutation.mutate(unflattenPaths(values), {
      onSuccess: () => toast.success(t("config.saved")),
    });
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Drift header — status pill plus, when drifted, the list of fields
          that have diverged between the effective target and what Telemt
          actually reports running. */}
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <SectionHeader title={t("config.tab")} />
          <DriftBadge status={drift.status} />
        </div>
        {drift.status === "drifted" && drift.fields.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-micro uppercase tracking-wider text-fg-muted">
              {t("config.drift.fieldsLabel")}
            </span>
            {drift.fields.map((f) => (
              <Badge key={f} variant="warn">
                {f}
              </Badge>
            ))}
          </div>
        )}
      </div>

      {/* U-13: clarify that the editor holds overrides, and empty fields
          inherit the effective value shown as a placeholder. */}
      <p className="text-micro text-fg-muted -mt-2">{t("config.overrideHint")}</p>

      {/* Override editor — the curated CONFIG_FIELDS, fully controlled. */}
      <ConfigSectionEditor
        values={values}
        effective={effectiveValues}
        onChange={(path, value) =>
          setValues((prev) => ({ ...prev, [path]: value }))
        }
        disabled={putMutation.isPending}
      />

      {/* Actions — Save persists the override, Apply pushes it to the node.
          Apply is gated on there being changed paths; ApplyConfigButton
          itself decides whether a restart-warning confirm is required. */}
      <div className="flex flex-wrap items-center gap-3 border-t border-divider pt-4">
        <Button onClick={handleSave} disabled={putMutation.isPending}>
          {t("config.save")}
        </Button>
        {changedPaths.length > 0 && (
          <span className="text-micro text-fg-muted">{t("config.unsavedHint")}</span>
        )}
        <ApplyConfigButton
          changedPaths={overridePaths}
          onApply={() => applyMutation.mutateAsync()}
          disabled={changedPaths.length > 0 || putMutation.isPending}
        />
      </div>
    </div>
  );
}
