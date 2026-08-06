// P4-T6: the fleet-group Config section's field editor — a "manage-fields"
// variant of the node's catalog-native ConfigTree (P4-T3/T4).
//
// The node page (ConfigTab) has an `observed` config to diff against, so
// every catalog field is always shown and editable (drift decorations tell
// the operator when a value diverges from what the node runs). The group
// page has no observed config at all: a group doesn't run anything, it just
// declares WHICH fields it governs. So this is not "ConfigTree with an
// empty observed" — it is a genuinely different mode with two lists:
//
//   - GOVERNED fields: paths present in `values` (the group's stored
//     sections). Each renders through the same `ConfigTreeField` row the
//     node page uses (so type-aware controls, apply-mode badges, and info
//     tooltips are shared, not reimplemented), plus a "remove from group"
//     button that drops the path from `values` entirely.
//   - ADDABLE fields: every other editable catalog path. Rendered as a
//     plain path + "add to group" button, deliberately with NO value
//     control — typing must never be possible before a field is explicitly
//     governed, otherwise "governed" would just mean "non-empty", which is
//     not what an operator opting a field into group management expects.
//
// We reuse `buildTree` + `ConfigTreeField` (the same building blocks
// ConfigTree.tsx composes) rather than importing `ConfigTree` itself:
// ConfigTree always renders the full catalog with no per-field
// governed/addable split, so there is no clean prop to bolt this mode onto
// without either duplicating its filtering logic outside the component or
// growing ConfigTreeField with group-only "add/remove" concerns it has no
// other use for. Keeping that split here instead leaves ConfigTree and
// ConfigTreeField untouched.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button, Input } from "@/ui";

import { buildTree, type TreeField } from "@/features/servers/config/buildTree";
import { ConfigTreeField } from "@/features/servers/config/ConfigTreeField";
import { unflattenPaths } from "@/features/servers/config/sections";
import type { ParamCatalogEntry } from "@/features/servers/config/paramCatalog";

export interface GovernedConfigFieldsProps {
  /** dotted-path → current value; a path's mere presence here means "governed". */
  values: Record<string, unknown>;
  onChange: (path: string, value: unknown) => void;
  /** Opts a catalog path into governance, seeded with a starting value. */
  onAdd: (path: string, value: unknown) => void;
  /** Drops a path out of governance entirely (not just clears its value). */
  onRemove: (path: string) => void;
  disabled?: boolean;
}

// A field the operator adds to the group starts at a sensible per-type
// default (parsed from the catalog's own documented default where one
// exists) rather than at `undefined` — an undefined value would render
// indistinguishably from "not governed", and would also get silently
// dropped by unflattenPaths' omit-empty rule on the next Save.
function defaultValueForEntry(entry: ParamCatalogEntry): unknown {
  switch (entry.type) {
    case "boolean":
      return entry.default === "true";
    case "number":
      return entry.default !== undefined ? Number(entry.default) : "";
    case "select":
      return entry.default ?? entry.options?.[0] ?? "";
    case "string[]":
      return [];
    case "string":
    default:
      return entry.default ?? "";
  }
}

function matchesSearch(field: TreeField, searchLower: string): boolean {
  if (!searchLower) return true;
  if (field.path.toLowerCase().includes(searchLower)) return true;
  return !!field.entry &&
    (field.entry.en.toLowerCase().includes(searchLower) ||
      field.entry.ru.toLowerCase().includes(searchLower));
}

export function GovernedConfigFields({
  values,
  onChange,
  onAdd,
  onRemove,
  disabled,
}: Readonly<GovernedConfigFieldsProps>) {
  const { t } = useTranslation("servers");
  const [addSearch, setAddSearch] = useState("");

  const governedPaths = useMemo(() => new Set(Object.keys(values)), [values]);

  // No observed config on the group page — buildTree's drift/locked
  // decorations are inert here (observed={} and groupPaths=∅), we only use
  // it for its catalog-entry lookup + section grouping.
  const sections = useMemo(
    () => buildTree(unflattenPaths(values), {}, new Set()),
    [values],
  );

  const governedSections = useMemo(
    () =>
      sections
        .map((s) => ({ section: s.section, fields: s.fields.filter((f) => governedPaths.has(f.path)) }))
        .filter((s) => s.fields.length > 0),
    [sections, governedPaths],
  );

  const addSearchLower = addSearch.trim().toLowerCase();
  const addableSections = useMemo(
    () =>
      sections
        .map((s) => ({
          section: s.section,
          // readonly excludes process-owned/container/unknown paths — a
          // field with no real catalog entry can't be governed.
          fields: s.fields.filter(
            (f) => !governedPaths.has(f.path) && !f.readonly && matchesSearch(f, addSearchLower),
          ),
        }))
        .filter((s) => s.fields.length > 0),
    [sections, governedPaths, addSearchLower],
  );

  function handleAdd(field: TreeField) {
    if (!field.entry) return;
    onAdd(field.path, defaultValueForEntry(field.entry));
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3">
        <h3 className="text-xs font-medium uppercase tracking-wider text-fg-muted">
          {t("config.tree.governedTitle")}
        </h3>
        {governedSections.length === 0 ? (
          <p className="text-xs text-fg-muted">{t("config.tree.noGoverned")}</p>
        ) : (
          governedSections.map(({ section, fields }) => (
            <details key={section} open className="rounded-md border border-divider p-3">
              <summary className="cursor-pointer text-sm font-medium text-fg">{section}</summary>
              <div className="mt-3 flex flex-col gap-4">
                {fields.map((field) => (
                  <div key={field.path} className="flex items-start gap-2">
                    <div className="min-w-0 flex-1">
                      <ConfigTreeField field={field} onChange={onChange} />
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={disabled}
                      onClick={() => onRemove(field.path)}
                    >
                      {t("config.tree.removeField")}
                    </Button>
                  </div>
                ))}
              </div>
            </details>
          ))
        )}
      </div>

      <div className="flex flex-col gap-3 border-t border-divider pt-4">
        <h3 className="text-xs font-medium uppercase tracking-wider text-fg-muted">
          {t("config.tree.addFieldTitle")}
        </h3>
        <Input
          type="search"
          value={addSearch}
          onChange={(e) => setAddSearch(e.target.value)}
          placeholder={t("config.tree.search")}
          className="w-full sm:w-64"
        />
        {addableSections.length === 0 ? (
          <p className="text-xs text-fg-muted">{t("config.tree.allGoverned")}</p>
        ) : (
          addableSections.map(({ section, fields }) => (
            <details key={section} className="rounded-md border border-divider p-3">
              <summary className="cursor-pointer text-sm font-medium text-fg">{section}</summary>
              <ul className="mt-3 flex flex-col gap-2">
                {fields.map((field) => (
                  <li
                    key={field.path}
                    className="flex items-center justify-between gap-2 rounded-xs border border-divider px-3 py-2"
                  >
                    <span className="truncate font-mono text-xs text-fg">{field.path}</span>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={disabled}
                      onClick={() => handleAdd(field)}
                    >
                      {t("config.tree.addField")}
                    </Button>
                  </li>
                ))}
              </ul>
            </details>
          ))
        )}
      </div>
    </div>
  );
}
