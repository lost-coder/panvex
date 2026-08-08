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
  hasPath,
  unflattenPaths,
} from "@/features/servers/config/sections";
import {
  readUpstreams, writeUpstreams, readMap, writeMap,
  type UpstreamEntry,
} from "@/features/servers/config/containers";
import { UpstreamsEditor } from "@/features/servers/config/UpstreamsEditor";
import { MapEditor } from "@/features/servers/config/MapEditor";

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

  // Контейнеры живут отдельно от плоской карты скаляров: их ключи задаёт
  // оператор и содержат точки, поэтому dotted-путь для них неоднозначен.
  //
  // F7: регрессия самой волны. F4 убрал из дерева строки контейнеров,
  // которые приходили из observed, а этот блок сеял контейнеры только из
  // desired. Для ноды, у которой контейнер есть в конфиге, но не в
  // desired-снапшоте, редактор оказывался пустым, read-only строки дерева
  // для него больше нет, а configDrift тоже молчит (он проекция desired на
  // observed) — конфигурация ноды переставала быть видна панели вообще
  // где-либо. Сеем каждый контейнер из desired, а при его отсутствии — из
  // observed, тем же правилом, каким дерево показывает скаляры
  // (value = hasDesired ? desired : observed, buildTree.ts).
  const initialContainers = useMemo(() => {
    const desired = data?.desired ?? {};
    const observed = data?.observed ?? {};
    // Контейнер, которого нет в desired, показывается по observed: иначе
    // реальная конфигурация ноды не видна нигде — строки дерева для
    // контейнеров убраны (их владельцы теперь эти редакторы), а дрейф молчит,
    // потому что он проекция desired на observed.
    const seed = (path: string) => (hasPath(desired, path) ? desired : observed);
    return {
      upstreams: readUpstreams(seed("upstreams")),
      dcOverrides: readMap(seed("dc_overrides"), "dc_overrides"),
      exclusiveMask: readMap(seed("censorship.exclusive_mask"), "censorship.exclusive_mask"),
    };
  }, [data?.desired, data?.observed]);
  const [containers, setContainers] = useState(initialContainers);

  // Paths the operator has edited but not yet saved — drives the dirty
  // state that blocks Apply (you save the override before pushing it).
  const changedPaths = useMemo(
    () => diffPaths(initialValues, values),
    [initialValues, values],
  );

  // F3: a container edit (UpstreamsEditor / MapEditor) is an unsaved change
  // too, exactly like a scalar one — `containers` vs. `initialContainers` is
  // the same comparison the reseed effect below already makes. Before this,
  // editing e.g. an upstream's weight showed no "unsaved" hint and did not
  // block Apply, so Apply could ship the last-SAVED container state while
  // the operator was staring at their own unsaved edit on screen.
  const containersChanged = useMemo(
    () => diffPaths(initialContainers, containers).length > 0,
    [initialContainers, containers],
  );
  const isDirty = changedPaths.length > 0 || containersChanged;

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
  // Same "don't clobber an unsaved edit" contract as `lastSeededRef`, but for
  // the container state — a background refetch must not wipe an in-progress
  // upstreams/dc_overrides/exclusive_mask edit any more than it wipes a
  // scalar one. `diffPaths` works fine here too: it only needs a
  // Record<string, unknown> and JSON.stringify-compares each key, which
  // covers the (small, three-key) container shape.
  const lastSeededContainersRef = useRef(initialContainers);
  const lastAgentIdRef = useRef(agentId);
  useEffect(() => {
    const idChanged = lastAgentIdRef.current !== agentId;
    lastAgentIdRef.current = agentId;
    const hasUnsavedEdits =
      diffPaths(lastSeededRef.current, values).length > 0 ||
      diffPaths(lastSeededContainersRef.current, containers).length > 0;
    if (!idChanged && hasUnsavedEdits) {
      // Refetch landed while the operator has unsaved edits on the SAME
      // server — keep their draft.
      return;
    }
    lastSeededRef.current = initialValues;
    lastSeededContainersRef.current = initialContainers;
    setValues(initialValues);
    setContainers(initialContainers);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, initialValues, initialContainers]);

  // What Apply will push: the persisted override's own paths. Feeding these
  // to ApplyConfigButton lets it decide whether a reload-confirmation dialog
  // is needed (e.g. a reload-mode field like censorship.tls_domain is set),
  // independent of the unsaved-edit diff above.
  //
  // F3: container paths belong here too — upstreams is reload-class (see
  // UpstreamsEditor's "reload" badge), so a persisted upstreams override
  // must be visible to ApplyConfigButton's reload decision the same way a
  // scalar reload-mode field already is. Only paths for containers that are
  // actually non-empty in the persisted override: an absent/empty container
  // isn't part of what Apply will push (see buildSections' F2 comment).
  const overridePaths = useMemo(() => {
    const paths = Object.keys(initialValues);
    if (initialContainers.upstreams.length > 0) paths.push("upstreams");
    if (Object.keys(initialContainers.dcOverrides).length > 0) paths.push("dc_overrides");
    if (Object.keys(initialContainers.exclusiveMask).length > 0) {
      paths.push("censorship.exclusive_mask");
    }
    return paths;
  }, [initialValues, initialContainers]);

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

  // Тело PUT: скаляры по dotted-путям + контейнеры, но только те, что
  // непусты или уже присутствуют в сохранённом снапшоте.
  //
  // Контейнер пишется, только если он непустой ИЛИ уже присутствует в
  // сохранённом снапшоте. Безусловная запись вписывала бы пустые upstreams /
  // dc_overrides / exclusive_mask на ноду, где их нет вовсе: configDrift —
  // проекция desired на observed, поэтому три несуществующие секции навсегда
  // уходили бы в дрейф, applyDiff не дал бы для пустых таблиц ни одного листа
  // (свести нечем), а `upstreams: []` доехал бы до ноды и стёр реальные
  // upstream-ы при перерисовке секции. Но если контейнер УЖЕ есть в desired
  // (даже пустым), его нужно писать и пустым — иначе оператор не сможет
  // очистить существующий контейнер.
  function buildSections(): Record<string, unknown> {
    const out = unflattenPaths(values);
    const stored = data?.desired ?? {};
    if (containers.upstreams.length > 0 || hasPath(stored, "upstreams")) {
      writeUpstreams(out, containers.upstreams);
    }
    if (Object.keys(containers.dcOverrides).length > 0 || hasPath(stored, "dc_overrides")) {
      writeMap(out, "dc_overrides", containers.dcOverrides);
    }
    if (
      Object.keys(containers.exclusiveMask).length > 0 ||
      hasPath(stored, "censorship.exclusive_mask")
    ) {
      writeMap(out, "censorship.exclusive_mask", containers.exclusiveMask);
    }
    return out;
  }

  function handleSave() {
    putMutation.mutate(buildSections(), {
      onSuccess: () => toast.success(t("config.saved")),
    });
  }

  // P4-T4: per-field drift actions.
  //
  // "Accept node value" pulls the drifted path's observed value into the
  // panel: a single-path PUT, which the panel merges into the existing
  // desired sections server-side (P2 Task 5) rather than replacing them.
  //
  // Fix (review round 1): the PUT alone left local `values` — and
  // `lastSeededRef`, the reseed baseline — untouched. If another path was
  // mid-edit (dirty), the reseed effect bails on refetch (it only resyncs
  // when NOTHING is dirty), so `values[path]` kept its stale pre-accept
  // value. A later Save then sent the WHOLE `values` map via
  // `unflattenPaths`, silently overwriting the just-accepted server value
  // back to the old one. Updating both `values` and `lastSeededRef` here,
  // optimistically, makes the accepted value stick locally too — so it
  // can't be clobbered by a later whole-map Save, and it isn't
  // misread as a fresh dirty edit either.
  function handleAcceptNode(path: string) {
    putMutation.mutate(unflattenPaths({ [path]: observedValues[path] }), {
      onSuccess: () => {
        setValues((prev) => ({ ...prev, [path]: observedValues[path] }));
        lastSeededRef.current = { ...lastSeededRef.current, [path]: observedValues[path] };
        toast.success(t("config.saved"));
      },
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
          fields and the drift decoration. `groupPaths` comes from
          `data.group_paths` (P4-T5) — the dotted paths the node's fleet
          group governs, so the tree can lock those fields. */}
      <ConfigTree
        desired={unflattenPaths(values)}
        observed={data.observed ?? {}}
        groupPaths={new Set(data.group_paths ?? [])}
        onChange={(path, value) =>
          setValues((prev) => ({ ...prev, [path]: value }))
        }
        onAcceptNode={handleAcceptNode}
        onRevertPanel={handleRevertPanel}
      />

      {/* Structural containers — upstreams / dc_overrides /
          censorship.exclusive_mask. Their keys are operator-chosen (and, for
          the two maps, can contain dots), so they live outside the dotted-
          path catalog the tree above edits; see containers.ts.

          MapEditor keeps a per-row text draft keyed by ROW INDEX while the
          operator types. A node switch forces this tab to re-seed
          `containers` unconditionally (see the effect above), which would
          otherwise leave MapEditor's internal draft state pointing at the
          PREVIOUS node's rows — masking the new node's value or attaching
          the stale draft to the wrong row. `key={agentId}` remounts the map
          editors on a node switch so no draft survives across that
          identity change. UpstreamsEditor has no internal state (fully
          controlled), so it doesn't need the same key. */}
      <section className="flex flex-col gap-3">
        <SectionHeader title={t("config.upstreams.title")} />
        <UpstreamsEditor
          value={containers.upstreams}
          onChange={(next: UpstreamEntry[]) => setContainers((p) => ({ ...p, upstreams: next }))}
        />
      </section>

      <section className="flex flex-col gap-3">
        <SectionHeader title={t("config.dcOverrides.title")} />
        <MapEditor
          key={agentId}
          value={containers.dcOverrides}
          onChange={(next) => setContainers((p) => ({ ...p, dcOverrides: next }))}
          keyLabel={t("config.dcOverrides.key")}
          valueLabel={t("config.dcOverrides.value")}
          valueKind="list"
        />
      </section>

      <section className="flex flex-col gap-3">
        <SectionHeader title={t("config.exclusiveMask.title")} />
        <MapEditor
          key={agentId}
          value={containers.exclusiveMask}
          onChange={(next) => setContainers((p) => ({ ...p, exclusiveMask: next }))}
          keyLabel={t("config.exclusiveMask.key")}
          valueLabel={t("config.exclusiveMask.value")}
          valueKind="scalar"
        />
      </section>

      {/* Actions — Save persists the override, Apply pushes it to the node.
          Apply is gated on there being changed paths; ApplyConfigButton
          itself decides whether a reload-confirmation dialog is required. */}
      <div className="flex flex-wrap items-center gap-3 border-t border-divider pt-4">
        <Button onClick={handleSave} disabled={putMutation.isPending}>
          {t("config.save")}
        </Button>
        {isDirty && (
          <span className="text-micro text-fg-muted">{t("config.unsavedHint")}</span>
        )}
        <ApplyConfigButton
          changedPaths={overridePaths}
          driftedFields={drift.status === "drifted" ? drift.fields : []}
          onApply={async (policy) => {
            const accepted = await applyMutation.mutateAsync(policy);
            setApplyBatchId(accepted.batch_id);
          }}
          disabled={isDirty || putMutation.isPending || applyBatchId !== null}
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
