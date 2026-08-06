// P5-T6: the Server-detail "Config" tab.
//
// Fetches the agent's config (desired / effective / observed + drift) via
// useAgentConfig, seeds a local editor map from DESIRED, and lets the
// operator edit the full catalog-native ConfigTree (P4). Save persists the
// desired sections (PUT, nested); Apply pushes them down to the running
// Telemt process. The drift header surfaces whether the observed config has
// diverged from the effective target, listing the diverging fields.
//
// The tree is fully controlled, so this tab owns the dotted-path → value
// map. We track which paths the user touched (changedPaths) against the
// initial flatten so the Apply gate only lights up — and the reload
// confirmation only fires — for genuinely-changed fields, surviving a
// Save→refetch round trip via the data-keyed reset effect.
//
// P4-T4: ConfigTree replaces the old ConfigSectionEditor + ObservedConfigViewer
// pair — the tree itself renders both the editable catalog fields AND every
// observed-only field Telemt reports (marked "unknown"), so there is no
// longer a separate read-only viewer below it. Two per-field drift actions
// are wired here: "Accept node value" PUTs the observed value at that one
// path (merges server-side); "Revert to panel value" applies just that path
// back down to the node.

import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Badge, Button, Spinner } from "@/ui";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { useToast } from "@/app/providers/ToastProvider";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import {
  useAgentConfig,
  useAgentConfigApplyBatch,
  useApplyAgentConfig,
  usePutAgentConfig,
} from "@/features/servers/config/configHooks";
import { ConfigTree } from "@/features/servers/config/ConfigTree";
import { DriftBadge } from "@/features/servers/config/DriftBadge";
import { ApplyConfigButton } from "@/features/servers/config/ApplyConfigButton";
import {
  flattenSections,
  unflattenPaths,
} from "@/features/servers/config/sections";

// Compute the set of dotted paths whose current value differs from the
// initial (override-seeded) flatten. Used both for the Apply gate and the
// reload-confirmation decision inside ApplyConfigButton.
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

  // P3-3.4: single Apply is a batch-of-one. Hold the launched batch id in state
  // and poll to terminal; the completion toast fires HERE now (the button is
  // kickoff-only).
  const [applyBatchId, setApplyBatchId] = useState<string | null>(null);
  const applyStatus = useAgentConfigApplyBatch(agentId, applyBatchId);
  const applyDone = applyStatus.data?.done === true;
  useEffect(() => {
    if (!applyBatchId || !applyDone) return;
    const status = applyStatus.data;
    if (!status) return;
    if (status.failed > 0) {
      const message = status.agents.find((a) => a.status === "failed")?.message ?? "";
      toast.error(t("config.apply.failed", { agent: server.name, error: message }));
    } else {
      toast.success(t("config.apply.applied", { count: status.applied }));
    }
    setApplyBatchId(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applyBatchId, applyDone]);

  // Editor state — seeded from the DESIRED snapshot. We keep the initial
  // flatten so the changed-path diff is stable across re-renders.
  const initialValues = useMemo(
    () => flattenSections(data?.desired ?? {}),
    [data?.desired],
  );
  // The observed config is what the node actually runs. ConfigTree falls
  // back to it for any path with no desired override, and "Accept node
  // value" reads the value to PUT from here too.
  const observedValues = useMemo(
    () => flattenSections(data?.observed ?? {}),
    [data?.observed],
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
  // to ApplyConfigButton lets it decide whether a reload-confirmation dialog
  // is needed (e.g. a reload-mode field like censorship.tls_domain is set),
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

  // P4-T4: per-field drift actions.
  //
  // "Accept node value" pulls the drifted path's observed value into the
  // panel: a single-path PUT, which the panel merges into the existing
  // desired sections server-side (P2 Task 5) rather than replacing them.
  function handleAcceptNode(path: string) {
    putMutation.mutate(unflattenPaths({ [path]: observedValues[path] }), {
      onSuccess: () => toast.success(t("config.saved")),
    });
  }

  // "Revert to panel value" pushes the panel's own value for just that one
  // path back down to the node — an apply restricted to `paths: [path]`
  // (P2 Task 7). It shares the same batch-id/toast machinery as the main
  // Apply button above, so only one apply is ever tracked at a time.
  async function handleRevertPanel(path: string) {
    const accepted = await applyMutation.mutateAsync({ paths: [path] });
    setApplyBatchId(accepted.batch_id);
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

      {/* P4-T4: the catalog-native config tree — replaces the curated
          editor + separate read-only viewer. Seeded from the in-progress
          edits (`unflattenPaths(values)`) rather than `data.desired`
          directly, so unsaved edits keep rendering across re-renders; the
          observed snapshot backs both the fallback value for un-overridden
          fields and the drift decoration. `groupPaths` stays empty until
          Task 5 adds `group_paths` to the agent-config API — see the
          plan's ordering note. */}
      <ConfigTree
        desired={unflattenPaths(values)}
        observed={data.observed ?? {}}
        groupPaths={new Set<string>()}
        onChange={(path, value) =>
          setValues((prev) => ({ ...prev, [path]: value }))
        }
        onAcceptNode={handleAcceptNode}
        onRevertPanel={handleRevertPanel}
      />

      {/* Actions — Save persists the override, Apply pushes it to the node.
          Apply is gated on there being changed paths; ApplyConfigButton
          itself decides whether a reload-confirmation dialog is required. */}
      <div className="flex flex-wrap items-center gap-3 border-t border-divider pt-4">
        <Button onClick={handleSave} disabled={putMutation.isPending}>
          {t("config.save")}
        </Button>
        {changedPaths.length > 0 && (
          <span className="text-micro text-fg-muted">{t("config.unsavedHint")}</span>
        )}
        <ApplyConfigButton
          changedPaths={overridePaths}
          driftedFields={drift.status === "drifted" ? drift.fields : []}
          onApply={async (policy) => {
            const accepted = await applyMutation.mutateAsync(policy);
            setApplyBatchId(accepted.batch_id);
          }}
          disabled={
            changedPaths.length > 0 || putMutation.isPending || applyBatchId !== null
          }
        />
        {applyBatchId !== null && (
          <span
            className="flex items-center gap-2 text-micro text-fg-muted"
            aria-live="polite"
          >
            <Spinner />
            {t("config.apply.inProgress")}
          </span>
        )}
      </div>
    </div>
  );
}
