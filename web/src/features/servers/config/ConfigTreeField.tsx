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
import { useId } from "react";
import { useTranslation } from "react-i18next";

import { Badge, type BadgeProps, Button, Input, Select, Toggle } from "@/ui";

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
  placeholder,
  onChange,
}: Readonly<{
  entry: ParamCatalogEntry;
  value: unknown;
  disabled: boolean;
  id?: string | undefined;
  placeholder?: string | undefined;
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
          placeholder={placeholder}
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
          placeholder={placeholder}
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
          placeholder={placeholder}
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
  const { entry, path, value, observed, drifted, locked, readonly, unknown, present } = field;
  const key = path.split(".").pop() ?? path;
  const processOwned = isProcessOwned(path);
  const disabled = readonly || locked;
  // Незаданное поле: Telemt его не сериализовал, значит на ноде действует
  // встроенный дефолт. Показываем дефолт приглушённым плейсхолдером, а не
  // пустым значением, — иначе панель утверждает, что поле пусто/выключено.
  const notSet = !present && !readonly;
  const defaultHint = entry?.default;
  // Explicit id/htmlFor (instead of FormField's cloneElement) so the
  // label↔control a11y association survives the two-column layout: the
  // control is no longer FormField's sole direct child.
  const fieldId = useId();

  function handleChange(next: unknown) {
    if (disabled) return;
    onChange(path, next);
  }

  return (
    // max-w bounds the form to a comfortable reading width so on an ultrawide
    // the control isn't stranded at the far edge with a huge middle gap.
    <div className="flex max-w-4xl flex-col gap-1.5">
      {/*
        Label + control: stacked on mobile (full-width control, reads fine on
        a phone), a two-column grid on sm+ (label/meta left, capped ~18rem
        control right) so a wide desktop panel isn't mostly empty input.
        The default-value hint sits under the label in the left column to keep
        each field to roughly one control-height row.
      */}
      <div className="flex flex-col gap-1.5 sm:grid sm:grid-cols-[minmax(0,1fr)_18rem] sm:items-center sm:gap-4">
        <div className="flex min-w-0 flex-col gap-1">
          <label htmlFor={fieldId} className="text-sm font-medium text-fg leading-none">
            <span className="inline-flex flex-wrap items-center gap-2">
              <span className="font-mono">{key}</span>
              {entry && <ApplyModeBadge applyMode={entry.applyMode} />}
              {entry && <InfoTooltip entry={entry} />}
            </span>
          </label>
          {notSet ? (
            <p className="text-caption text-fg-muted">
              {defaultHint !== undefined
                ? t("config.tree.notSetWithDefault", { value: defaultHint })
                : t("config.tree.notSet")}
            </p>
          ) : (
            defaultHint !== undefined && (
              <p className="text-caption text-fg-muted">
                {t("config.tree.defaultHint", { value: defaultHint })}
              </p>
            )
          )}
        </div>
        <div className="min-w-0">
          {entry ? (
            <div className={notSet ? "opacity-60" : undefined}>
              <FieldControl
                entry={entry}
                value={value}
                disabled={disabled}
                id={fieldId}
                placeholder={notSet ? defaultHint : undefined}
                onChange={handleChange}
              />
            </div>
          ) : (
            <RawValueInput value={value} disabled={disabled} id={fieldId} />
          )}
        </div>
      </div>

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
