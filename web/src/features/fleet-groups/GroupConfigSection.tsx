// P5-T7: the Fleet-group "Config" section on the group detail page.
//
// Mirrors the Server-detail ConfigTab, but targets the GROUP config: it
// edits the group's config TARGET (the sections the panel pushes to every
// node in the group) and applies it as a rolling rollout. Instead of a
// single-node drift header, it renders a per-node drift summary so the
// operator can see, at a glance, which nodes are in sync with the target.
//
// The editor is fully controlled, so this section owns the dotted-path →
// value map. We track which paths the user touched (changedPaths) against
// the initial flatten so the Apply gate only lights up — and the
// restart-warning only fires — for genuinely-changed fields, surviving a
// Save→refetch round trip via the data-keyed reset effect.

import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";

import { Button, Spinner } from "@/ui";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { useToast } from "@/app/providers/ToastProvider";
import { useUnsavedChangesGuard } from "@/shared/hooks";
import { useServersList } from "@/features/servers/hooks/useServersList";

import {
  useApplyGroupConfig,
  useGroupConfig,
  usePutGroupConfig,
} from "@/features/servers/config/configHooks";
import { ConfigSectionEditor } from "@/features/servers/config/ConfigSectionEditor";
import {
  DriftBadge,
  type DriftStatus,
} from "@/features/servers/config/DriftBadge";
import { ApplyConfigButton } from "@/features/servers/config/ApplyConfigButton";
import {
  flattenSections,
  unflattenPaths,
} from "@/features/servers/config/sections";

// Compute the set of dotted paths whose current value differs from the
// initial (target-seeded) flatten. Used both for the Apply gate and the
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

// The group node status comes off the wire as a free-form string; narrow it
// to the DriftBadge's known set, defaulting anything unexpected to "unknown".
const KNOWN_DRIFT: ReadonlySet<string> = new Set<DriftStatus>([
  "in_sync",
  "drifted",
  "unknown",
]);

function toDriftStatus(status: string): DriftStatus {
  return KNOWN_DRIFT.has(status) ? (status as DriftStatus) : "unknown";
}

export function GroupConfigSection({ groupId }: Readonly<{ groupId: string }>) {
  const { t } = useTranslation("servers");
  const toast = useToast();
  const navigate = useNavigate();

  // The drift payload carries agent_id only (groupConfigNodeDrift on the
  // backend has no node_name) — resolve display names from the cached
  // agents list instead of showing raw UUIDs (audit E5).
  const { servers } = useServersList();
  const nameById = useMemo(
    () => new Map(servers.map((s) => [s.id, s.name])),
    [servers],
  );

  const { data, isLoading, isError } = useGroupConfig(groupId);
  const putMutation = usePutGroupConfig(groupId);
  const applyMutation = useApplyGroupConfig(groupId);

  // Editor state — seeded from the group TARGET. We keep the initial flatten
  // so the changed-path diff is stable across re-renders.
  const initialValues = useMemo(
    () => flattenSections(data?.sections ?? {}),
    [data?.sections],
  );
  const [values, setValues] = useState<Record<string, unknown>>(initialValues);

  // Re-seed the editor whenever a fresh target arrives (initial load or a
  // post-Save / post-Apply refetch). Keyed on the flattened initial so a
  // payload with an identical target doesn't clobber in-flight edits.
  useEffect(() => {
    setValues(initialValues);
  }, [initialValues]);

  // Paths the operator has edited but not yet saved — drives the dirty state
  // that blocks Apply (you save the target before rolling it out).
  const changedPaths = useMemo(
    () => diffPaths(initialValues, values),
    [initialValues, values],
  );

  // Audit E4: guard in-app navigation while there are unsaved config changes.
  useUnsavedChangesGuard(changedPaths.length > 0);

  // What Apply will roll out: the persisted target's own paths. Feeding these
  // to ApplyConfigButton lets it decide whether a restart-warning confirm is
  // needed (e.g. a restart-only field like censorship.tls_domain is set),
  // independent of the unsaved-edit diff above.
  const targetPaths = useMemo(() => Object.keys(initialValues), [initialValues]);

  if (isLoading) {
    return (
      <section className="rounded-xs bg-bg-card border border-divider p-4">
        <SectionHeader title={t("config.tab")} />
        <div
          className="flex items-center justify-center gap-2 py-6 text-xs text-fg-muted"
          aria-busy
          aria-live="polite"
        >
          <Spinner />
          {t("loading.tab")}
        </div>
      </section>
    );
  }

  if (isError || !data) {
    return (
      <section className="rounded-xs bg-bg-card border border-divider p-4">
        <SectionHeader title={t("config.tab")} />
        <p className="py-6 text-center text-sm text-status-error">
          {t("error.requestFailed")}
        </p>
      </section>
    );
  }

  const nodes = data.nodes;

  function handleSave() {
    putMutation.mutate(unflattenPaths(values), {
      onSuccess: () => toast.success(t("config.saved")),
    });
  }

  return (
    <section className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-6">
      <SectionHeader title={t("config.tab")} />

      {/* Group target editor — the curated CONFIG_FIELDS, fully controlled. */}
      <ConfigSectionEditor
        values={values}
        onChange={(path, value) =>
          setValues((prev) => ({ ...prev, [path]: value }))
        }
        disabled={putMutation.isPending}
      />

      {/* Actions — Save persists the group target, Apply rolls it out to every
          node. Apply is gated on there being no unsaved edits; the rolling
          ApplyResult (applied / failed) is surfaced by ApplyConfigButton. */}
      <div className="flex flex-wrap items-center gap-3 border-t border-divider pt-4">
        <Button onClick={handleSave} disabled={putMutation.isPending}>
          {t("config.save")}
        </Button>
        {changedPaths.length > 0 && (
          <span className="text-micro text-fg-muted">{t("config.unsavedHint")}</span>
        )}
        <ApplyConfigButton
          changedPaths={targetPaths}
          onApply={() => applyMutation.mutateAsync()}
          labelKey="config.apply.buttonGroup"
          disabled={changedPaths.length > 0 || putMutation.isPending}
        />
      </div>

      {/* Per-node drift summary — each node in the group with its current
          alignment to the target. */}
      <div className="flex flex-col gap-2 border-t border-divider pt-4">
        <SectionHeader title={t("config.nodesTitle")} badge={nodes.length} />
        {nodes.length === 0 ? (
          <p className="text-xs text-fg-muted">{t("config.noNodes")}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {nodes.map((node) => (
              <li
                key={node.agent_id}
                className="flex items-center justify-between gap-3 rounded-xs border border-divider px-3 py-2"
              >
                <button
                  type="button"
                  onClick={() =>
                    void navigate({ to: "/servers/$serverId", params: { serverId: node.agent_id } })
                  }
                  className="font-mono text-xs text-fg truncate text-left hover:text-accent hover:underline"
                  title={node.agent_id}
                >
                  {nameById.get(node.agent_id) ?? node.agent_id}
                </button>
                <DriftBadge status={toDriftStatus(node.status)} />
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}
