// P4-T2: one field row in the catalog-native config tree. Renders the raw
// dotted-path key as a monospace label, the LIVE/RELOAD apply-mode badge,
// an InfoTooltip with the localized param description, a default-value
// hint, the type-appropriate control (reusing the boolean/number/select/
// string/string[] switch that ConfigSectionEditor.tsx pioneered), and the
// lock/drift/unknown/process-owned decorations that buildTree.ts (Task 1)
// computes per field.
//
// Process-owned paths (general.data_path, general.quota_state_path,
// general.disable_colors) are filtered out of PARAM_CATALOG, so buildTree
// reports them with BOTH unknown:true and readonly:true — the same shape
// a genuinely-unknown observed-only path gets. isProcessOwned(path) is the
// tie-breaker: it takes precedence over the "unknown parameter" note so
// operators see "set at process start", not "unknown parameter", for
// fields that are actually documented but structurally uneditable here.
import { useTranslation } from "react-i18next";

import { Badge, type BadgeProps, Button, FormField, Input, Select, Toggle } from "@/ui";

import { InfoTooltip } from "./InfoTooltip";
import { isProcessOwned } from "./guard";
import type { TreeField } from "./buildTree";
import type { ParamCatalogEntry } from "./paramCatalog";

export interface ConfigTreeFieldProps {
  field: TreeField;
  onChange: (path: string, value: unknown) => void;
  /**
   * P4-T4: the fleet group's display name, interpolated into the
   * "Set by group {{name}}" locked note. ConfigTab doesn't have this on
   * hand yet (the `server` prop carries no group name), so callers may
   * omit it — the note falls back to a generic "this group" rather than
   * ever rendering the raw `{{name}}` placeholder unresolved.
   */
  groupName?: string | undefined;
  /**
   * P4-T4: per-field drift-resolution actions, shown ONLY on a drifted
   * field. Optional so ConfigTreeField stays usable standalone (existing
   * tests, any future read-only embedding) without wiring both handlers.
   */
  onAcceptNode?: ((path: string) => void) | undefined;
  onRevertPanel?: ((path: string) => void) | undefined;
}

function listToText(value: unknown): string {
  return Array.isArray(value) ? (value as unknown[]).map(String).join(", ") : "";
}

function textToList(text: string): string[] {
  return text
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

function ApplyModeBadge({ applyMode }: Readonly<{ applyMode: ParamCatalogEntry["applyMode"] }>) {
  const { t } = useTranslation("servers");
  const isReload = applyMode === "reload";
  const variant: NonNullable<BadgeProps["variant"]> = isReload ? "warn" : "ok";
  return (
    <Badge variant={variant} title={t(isReload ? "config.badge.reloadHint" : "config.badge.hotHint")}>
      {t(isReload ? "config.badge.reload" : "config.badge.hot")}
    </Badge>
  );
}

function FieldControl({
  entry,
  value,
  disabled,
  id,
  onChange,
}: Readonly<{
  entry: ParamCatalogEntry;
  value: unknown;
  disabled: boolean;
  id?: string | undefined;
  onChange: (value: unknown) => void;
}>) {
  switch (entry.type) {
    case "boolean":
      return <Toggle id={id} checked={value === true} onChange={onChange} disabled={disabled} />;
    case "number":
      return (
        <Input
          id={id}
          type="number"
          value={value === undefined || value === null ? "" : String(value)}
          disabled={disabled}
          onChange={(e) => {
            const raw = e.target.value;
            onChange(raw === "" ? "" : Number(raw));
          }}
        />
      );
    case "select":
      return (
        <Select
          id={id}
          value={typeof value === "string" ? value : ""}
          disabled={disabled}
          onChange={onChange}
          options={(entry.options ?? []).map((o) => ({ value: o, label: o }))}
        />
      );
    case "string[]":
      return (
        <Input
          id={id}
          type="text"
          value={listToText(value)}
          disabled={disabled}
          onChange={(e) => onChange(textToList(e.target.value))}
        />
      );
    case "string":
    default:
      return (
        <Input
          id={id}
          type="text"
          value={typeof value === "string" ? value : ""}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value)}
        />
      );
  }
}

/** Renders raw values for unknown/container fields — no catalog entry means no type to key off. */
function RawValueInput({
  value,
  disabled,
  id,
}: Readonly<{ value: unknown; disabled: boolean; id?: string | undefined }>) {
  const text = value === undefined || value === null ? "" : String(value);
  return <Input id={id} type="text" value={text} disabled={disabled} onChange={() => {}} />;
}

export function ConfigTreeField({
  field,
  onChange,
  groupName,
  onAcceptNode,
  onRevertPanel,
}: Readonly<ConfigTreeFieldProps>) {
  const { t } = useTranslation("servers");
  const { entry, path, value, observed, drifted, locked, readonly, unknown } = field;
  const key = path.split(".").pop() ?? path;
  const processOwned = isProcessOwned(path);
  const disabled = readonly || locked;

  function handleChange(next: unknown) {
    if (disabled) return;
    onChange(path, next);
  }

  return (
    <div className="flex flex-col gap-1.5">
      {/*
        FormField's cloneElement a11y wiring (form-field.tsx) reaches only a
        SINGLE direct child, injecting its generated id onto it so the
        label's htmlFor resolves to a real focusable element. The control
        (FieldControl/RawValueInput) must therefore be that sole child —
        decorations render as siblings below, outside FormField, exactly
        like ConfigSectionEditor.tsx's FieldInput usage.
      */}
      <FormField
        label={
          <span className="inline-flex items-center gap-2">
            <span className="font-mono">{key}</span>
            {entry && <ApplyModeBadge applyMode={entry.applyMode} />}
            {entry && <InfoTooltip entry={entry} />}
          </span>
        }
      >
        {entry ? (
          <FieldControl entry={entry} value={value} disabled={disabled} onChange={handleChange} />
        ) : (
          <RawValueInput value={value} disabled={disabled} />
        )}
      </FormField>

      {entry?.default !== undefined && (
        <p className="text-caption text-fg-muted">
          {t("config.tree.defaultHint", { value: entry.default })}
        </p>
      )}

      {drifted && (
        <div className="flex flex-col gap-1.5">
          <p className="text-caption text-status-warn">
            {t("config.tree.driftPanel", { value: String(value) })}
            {" / "}
            {t("config.tree.driftNode", { value: String(observed) })}
          </p>
          {(onAcceptNode || onRevertPanel) && (
            <div className="flex flex-wrap gap-2">
              {onAcceptNode && (
                <Button type="button" variant="outline" size="sm" onClick={() => onAcceptNode(path)}>
                  {t("config.tree.acceptNode")}
                </Button>
              )}
              {onRevertPanel && (
                <Button type="button" variant="outline" size="sm" onClick={() => onRevertPanel(path)}>
                  {t("config.tree.revertPanel")}
                </Button>
              )}
            </div>
          )}
        </div>
      )}

      {locked && (
        <p className="text-caption text-fg-muted">
          {t("config.tree.lockedByGroup", { name: groupName ?? "this group" })}
        </p>
      )}

      {processOwned ? (
        <p className="text-caption text-fg-muted">{t("config.tree.processOwned")}</p>
      ) : unknown ? (
        <p className="text-caption text-fg-muted">{t("config.tree.unknownParam")}</p>
      ) : (
        readonly &&
        !locked && <p className="text-caption text-fg-muted">{t("config.tree.readonlyContainer")}</p>
      )}
    </div>
  );
}
