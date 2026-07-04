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

import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";

import { Button, Spinner } from "@/ui";
import { StatusPill } from "@/ui/primitives/StatusPill";
import type { PillTone } from "@/ui/tokens/colors";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { useToast } from "@/app/providers/ToastProvider";
import { useUnsavedChangesGuard } from "@/shared/hooks";
import { useServersList } from "@/features/servers/hooks/useServersList";
import type { GroupApplyAgentStatus } from "@/shared/api/schemas/config";

import {
  useActiveGroupConfigApplyBatch,
  useApplyGroupConfig,
  useGroupConfig,
  useGroupConfigApplyBatch,
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

// Map an async apply status to a StatusPill tone (color-blind-safe glyph
// rides along) so partial failures read at a glance: a failed agent is a
// red pill, a succeeded one green, in-flight ones neutral/amber. "halted"
// is a BATCH-level status only (a rolling rollout stopped after too many
// failures) — there is no per-agent "halted", those targets read "skipped"
// — so the map is keyed by the union of both vocabularies and reused for
// the overall rollout pill as well as the per-agent ones.
type ApplyDisplayStatus = GroupApplyAgentStatus["status"] | "halted";

const APPLY_TONE: Record<ApplyDisplayStatus, PillTone> = {
  pending: "neutral",
  running: "warn",
  succeeded: "ok",
  failed: "error",
  skipped: "neutral",
  halted: "error",
};

const APPLY_GLYPH: Record<ApplyDisplayStatus, string> = {
  pending: "•",
  running: "…",
  succeeded: "✓",
  failed: "✕",
  skipped: "⊘",
  halted: "■",
};

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

  // RESUMABLE rollout view (Phase A / A6): on mount, ask the backend
  // whether this group already has a batch in flight — this is what makes
  // the rollout survive a page reload or being opened from a different
  // device, since a fresh mount has no React state to remember a batch id.
  // `startedBatchId` covers the OTHER case: an Apply kicked off in THIS
  // session, before the active-batch query has had a chance to refetch.
  // Either way, the per-agent panel is always driven by a batch id fed to
  // useGroupConfigApplyBatch, which polls the PERSISTENT status endpoint —
  // never by the job handles from a single 202 response.
  const activeBatch = useActiveGroupConfigApplyBatch(groupId);
  const [startedBatchId, setStartedBatchId] = useState<string | null>(null);
  const batchId = startedBatchId ?? activeBatch.data?.batch_id ?? null;
  const applyStatus = useGroupConfigApplyBatch(groupId, batchId);

  // Kick off the async apply and remember the batch so polling can start.
  async function startApply() {
    const accepted = await applyMutation.mutateAsync();
    setStartedBatchId(accepted.batch_id);
  }

  // Surface a terminal rollout via the global toast once, when done flips.
  const rollout = applyStatus.data;
  useEffect(() => {
    if (!rollout?.done) return;
    if (rollout.status === "halted") {
      toast.error(
        t("config.apply.halted", {
          applied: rollout.applied,
          total: rollout.total,
          skipped: rollout.skipped,
        }),
      );
    } else if (rollout.failed > 0) {
      toast.error(
        t("config.apply.partial", {
          applied: rollout.applied,
          failed: rollout.failed,
          total: rollout.total,
        }),
      );
    } else {
      toast.success(t("config.apply.done", { count: rollout.total }));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rollout?.done]);

  // Editor state — seeded from the group TARGET. We keep the initial flatten
  // so the changed-path diff is stable across re-renders.
  const initialValues = useMemo(
    () => flattenSections(data?.sections ?? {}),
    [data?.sections],
  );
  const [values, setValues] = useState<Record<string, unknown>>(initialValues);

  // Paths the operator has edited but not yet saved — drives the dirty state
  // that blocks Apply (you save the target before rolling it out).
  const changedPaths = useMemo(
    () => diffPaths(initialValues, values),
    [initialValues, values],
  );

  // 3.12: re-seed the editor from a fresh target on initial load, on a
  // genuine identity change (switched to a different group), or on a
  // post-Save/post-Apply refetch where the operator has no unsaved edits.
  // Previously this ran on every `initialValues` change unconditionally —
  // a background refetch (including the WS seq-gap full-cache
  // invalidation, or the async-apply status poll) while the operator was
  // mid-edit would silently wipe their unsaved changes.
  //
  // `lastSeededRef` snapshots the initialValues the editor was last reset
  // to. Dirtiness is `values` vs. THAT snapshot, not vs. the just-arrived
  // `initialValues` — on the render where `groupId` changes, `values`
  // still holds the previous group's draft, so diffing it against the
  // brand-new `initialValues` would spuriously read as "dirty" and block
  // the very re-seed a group-id change is supposed to force.
  const lastSeededRef = useRef(initialValues);
  const lastGroupIdRef = useRef(groupId);
  useEffect(() => {
    const idChanged = lastGroupIdRef.current !== groupId;
    lastGroupIdRef.current = groupId;
    if (!idChanged && diffPaths(lastSeededRef.current, values).length > 0) {
      // Refetch landed while the operator has unsaved edits on the SAME
      // group — keep their draft.
      return;
    }
    lastSeededRef.current = initialValues;
    setValues(initialValues);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupId, initialValues]);

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
          progress + terminal toast are surfaced from the batch status below. */}
      <div className="flex flex-wrap items-center gap-3 border-t border-divider pt-4">
        <Button onClick={handleSave} disabled={putMutation.isPending}>
          {t("config.save")}
        </Button>
        {changedPaths.length > 0 && (
          <span className="text-micro text-fg-muted">{t("config.unsavedHint")}</span>
        )}
        <ApplyConfigButton
          changedPaths={targetPaths}
          onApply={startApply}
          labelKey="config.apply.buttonGroup"
          disabled={
            changedPaths.length > 0 ||
            putMutation.isPending ||
            applyMutation.isPending ||
            (applyStatus.isFetching && !rollout?.done)
          }
        />
      </div>

      {/* Async rollout progress — per-agent status while the apply is in
          flight (and its terminal summary), so a PARTIAL failure is visible
          per node rather than hidden behind a single toast. Rendered only
          once a batch has been kicked off. */}
      {rollout && rollout.agents.length > 0 && (
        <div className="flex flex-col gap-2 border-t border-divider pt-4">
          <SectionHeader
            title={t("config.apply.progressTitle")}
            badge={`${rollout.applied}/${rollout.total}`}
            trailing={
              rollout.status === "halted" ? (
                <StatusPill
                  tone={APPLY_TONE.halted}
                  glyph={APPLY_GLYPH.halted}
                  label={t("config.apply.statusHalted")}
                />
              ) : undefined
            }
          />
          {rollout.skipped > 0 && (
            <p className="text-micro text-fg-muted">
              {t("config.apply.skipped", { count: rollout.skipped })}
            </p>
          )}
          <ul className="flex flex-col gap-2" aria-live="polite">
            {rollout.agents.map((agent) => (
              <li
                key={agent.agent_id}
                className="flex items-center justify-between gap-3 rounded-xs border border-divider px-3 py-2"
              >
                <span className="flex flex-col min-w-0">
                  <span
                    className="font-mono text-xs text-fg truncate"
                    title={agent.agent_id}
                  >
                    {nameById.get(agent.agent_id) ?? agent.agent_id}
                  </span>
                  {/* Persisted failure reason (survives job eviction / a
                      reload) — shown inline rather than only on hover so a
                      resumed view is actionable at a glance. */}
                  {agent.message && (
                    <span
                      className="text-micro text-status-error truncate"
                      title={agent.message}
                    >
                      {agent.message}
                    </span>
                  )}
                </span>
                <StatusPill
                  tone={APPLY_TONE[agent.status]}
                  glyph={APPLY_GLYPH[agent.status]}
                  label={t(
                    `config.apply.status${
                      agent.status.charAt(0).toUpperCase() + agent.status.slice(1)
                    }`,
                  )}
                />
              </li>
            ))}
          </ul>
        </div>
      )}

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
