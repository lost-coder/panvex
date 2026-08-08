// P4-T3: the collapsible, searchable/filterable tree that assembles
// buildTree's TreeSection[] into a UI. Each section is a native
// <details>/<summary> (no accordion primitive in the kit — see
// ConfigTreeField.tsx's comment style for the same pragmatic reuse), with a
// search box and two toggle-button filters ("changed only" / "drifted
// only") above the list.
//
// Container rows (upstreams, dc_overrides, censorship.exclusive_mask):
// since the container split (Task 2 — see paramCatalog.ts/containers.ts),
// PARAM_CATALOG no longer lists these paths at all, so buildTree.ts only
// ever produces a TreeField for one of them when the node's *observed*
// config actually reports data there — a phantom no-data row (readonly with
// both value and observed undefined) can no longer occur for them, so no
// filtering is needed here. A container path that DOES carry data renders
// as an ordinary readonly row via the existing readonlyContainer note in
// ConfigTreeField; a real structured editor (UpstreamsEditor/MapEditor,
// backed by containers.ts) is out of scope for this component.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button, Input } from "@/ui";
import type { ConfigSections } from "@/shared/api/schemas/config";

import { buildTree } from "./buildTree";
import { ConfigTreeField } from "./ConfigTreeField";

export interface ConfigTreeProps {
  desired: ConfigSections;
  observed: ConfigSections;
  groupPaths: Set<string>;
  onChange: (path: string, value: unknown) => void;
  /** P4-T4: threaded through to ConfigTreeField's locked-note interpolation. */
  groupName?: string | undefined;
  /** P4-T4: per-field drift-resolution actions, threaded through to ConfigTreeField. */
  onAcceptNode?: ((path: string) => void) | undefined;
  onRevertPanel?: ((path: string) => void) | undefined;
}

function snapshotValues(desired: ConfigSections, observed: ConfigSections, groupPaths: Set<string>) {
  const map = new Map<string, unknown>();
  for (const section of buildTree(desired, observed, groupPaths)) {
    for (const field of section.fields) map.set(field.path, field.value);
  }
  return map;
}

export function ConfigTree({
  desired,
  observed,
  groupPaths,
  onChange,
  groupName,
  onAcceptNode,
  onRevertPanel,
}: Readonly<ConfigTreeProps>) {
  const { t } = useTranslation("servers");
  const [search, setSearch] = useState("");
  const [changedOnly, setChangedOnly] = useState(false);
  const [driftedOnly, setDriftedOnly] = useState(false);

  // Snapshotted once on mount ("changed" means "differs from what the tree
  // showed when this view first opened"), not recomputed on every prop
  // change — otherwise an edit that flows back through `desired` would
  // immediately re-baseline itself and "changed only" would never catch it.
  const [initialValues] = useState<Map<string, unknown>>(() =>
    snapshotValues(desired, observed, groupPaths),
  );

  const sections = useMemo(
    () => buildTree(desired, observed, groupPaths),
    [desired, observed, groupPaths],
  );

  const searchLower = search.trim().toLowerCase();

  const filteredSections = useMemo(() => {
    return sections
      .map((section) => ({
        section: section.section,
        fields: section.fields.filter((field) => {
          if (searchLower) {
            const matchesPath = field.path.toLowerCase().includes(searchLower);
            const matchesEntry =
              !!field.entry &&
              (field.entry.en.toLowerCase().includes(searchLower) ||
                field.entry.ru.toLowerCase().includes(searchLower));
            if (!matchesPath && !matchesEntry) return false;
          }

          if (changedOnly && field.value === initialValues.get(field.path)) return false;
          if (driftedOnly && !field.drifted) return false;

          return true;
        }),
      }))
      .filter((section) => section.fields.length > 0);
  }, [sections, searchLower, changedOnly, driftedOnly, initialValues]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t("config.tree.search")}
          className="w-full sm:w-64"
        />
        <Button
          type="button"
          variant={changedOnly ? "default" : "outline"}
          size="sm"
          aria-pressed={changedOnly}
          onClick={() => setChangedOnly((v) => !v)}
        >
          {t("config.tree.filterChanged")}
        </Button>
        <Button
          type="button"
          variant={driftedOnly ? "default" : "outline"}
          size="sm"
          aria-pressed={driftedOnly}
          onClick={() => setDriftedOnly((v) => !v)}
        >
          {t("config.tree.filterDrifted")}
        </Button>
      </div>

      {filteredSections.length === 0 ? (
        <p className="text-sm text-fg-muted">{t("config.tree.emptyFiltered")}</p>
      ) : (
        filteredSections.map(({ section, fields }) => (
          <details key={section} open className="rounded-md border border-divider p-3">
            <summary className="cursor-pointer text-sm font-medium text-fg">{section}</summary>
            <div className="mt-3 flex flex-col gap-4">
              {fields.map((field) => (
                <ConfigTreeField
                  key={field.path}
                  field={field}
                  onChange={onChange}
                  groupName={groupName}
                  onAcceptNode={onAcceptNode}
                  onRevertPanel={onRevertPanel}
                />
              ))}
            </div>
          </details>
        ))
      )}
    </div>
  );
}
