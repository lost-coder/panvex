// P5-T5: presentational, fully-controlled editor for the curated Telemt
// config fields. It renders the CONFIG_FIELDS registry grouped by
// section, with the right @/ui input for each field type and a small
// hot/restart apply-mode badge next to every label.
//
// It is intentionally dumb: it holds no state and does no data fetching.
// The parent owns the dotted-path → value map and feeds changes back in
// via onChange(path, value). Number fields emit numbers, boolean fields
// emit booleans, and string[] fields are edited as a comma-separated
// text field that maps to/from a string array (v1 — a richer tag input
// can replace this later without changing the contract).

import { useTranslation } from "react-i18next";

import { Badge, type BadgeProps, FormField, Input, Select, Toggle } from "@/ui";

import {
  type ConfigField,
  fieldsBySection,
} from "./fieldRegistry";

export interface ConfigSectionEditorProps {
  /** dotted-path → current value (typically from flattenSections). */
  values: Record<string, unknown>;
  /**
   * U-13: dotted-path → effective value. Shown as the input placeholder for
   * fields the operator hasn't overridden, so an empty box reads as "inherits
   * the current effective value" rather than "will blank this setting".
   */
  effective?: Record<string, unknown> | undefined;
  onChange: (path: string, value: unknown) => void;
  disabled?: boolean;
}

/** Render an effective value as a placeholder string (skip empty/objects). */
function placeholderFor(v: unknown): string | undefined {
  if (v === undefined || v === null || v === "") return undefined;
  if (Array.isArray(v)) return v.map(String).join(", ");
  if (typeof v === "object") return undefined;
  return String(v);
}

/** Apply-mode badge — "Live" (hot) vs "Restart" with an explanatory tooltip. */
function ApplyModeBadge({ field }: Readonly<{ field: ConfigField }>) {
  const { t } = useTranslation("servers");
  const isRestart = field.applyMode === "restart";
  const variant: NonNullable<BadgeProps["variant"]> = isRestart ? "warn" : "ok";
  const label = t(isRestart ? "config.badge.restart" : "config.badge.hot");
  const hint = t(isRestart ? "config.badge.restartHint" : "config.badge.hotHint");
  return (
    <Badge variant={variant} title={hint}>
      {label}
    </Badge>
  );
}

/** Comma-separated text editor for a string[] field. */
function listToText(value: unknown): string {
  return Array.isArray(value) ? (value as unknown[]).map(String).join(", ") : "";
}

function textToList(text: string): string[] {
  return text
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

function FieldInput({
  field,
  value,
  placeholder,
  onChange,
  disabled,
  id,
}: Readonly<{
  field: ConfigField;
  value: unknown;
  placeholder?: string | undefined;
  onChange: (path: string, value: unknown) => void;
  disabled?: boolean | undefined;
  // Injected by FormField via cloneElement so the generated label's
  // htmlFor resolves to the real focusable control (a11y association).
  id?: string | undefined;
}>) {
  switch (field.type) {
    case "boolean":
      return (
        <Toggle
          id={id}
          checked={value === true}
          onChange={(checked) => onChange(field.path, checked)}
          disabled={disabled ?? false}
        />
      );
    case "number":
      return (
        <Input
          id={id}
          type="number"
          value={value === undefined || value === null ? "" : String(value)}
          placeholder={placeholder}
          disabled={disabled}
          onChange={(e) => {
            const raw = e.target.value;
            onChange(field.path, raw === "" ? "" : Number(raw));
          }}
        />
      );
    case "select":
      return (
        <Select
          id={id}
          value={typeof value === "string" ? value : ""}
          disabled={disabled}
          onChange={(v) => onChange(field.path, v)}
          options={(field.options ?? []).map((o) => ({ value: o, label: o }))}
        />
      );
    case "string[]":
      return (
        <Input
          id={id}
          type="text"
          value={listToText(value)}
          placeholder={placeholder}
          disabled={disabled}
          onChange={(e) => onChange(field.path, textToList(e.target.value))}
        />
      );
    case "string":
    default:
      return (
        <Input
          id={id}
          type="text"
          value={typeof value === "string" ? value : ""}
          placeholder={placeholder}
          disabled={disabled}
          onChange={(e) => onChange(field.path, e.target.value)}
        />
      );
  }
}

export function ConfigSectionEditor({
  values,
  effective,
  onChange,
  disabled,
}: Readonly<ConfigSectionEditorProps>) {
  const { t } = useTranslation("servers");
  const bySection = fieldsBySection();

  return (
    <div className="flex flex-col gap-8">
      {Object.entries(bySection).map(([section, fields]) => (
        <section key={section} className="flex flex-col gap-4">
          <h3 className="text-xs font-medium uppercase tracking-wider text-fg-muted">
            {t(`config.section.${section}`, { defaultValue: section })}
          </h3>
          <div className="flex flex-col gap-5">
            {fields.map((field) => (
              <FormField
                key={field.path}
                label={
                  <span className="inline-flex items-center gap-2">
                    {t(field.labelKey)}
                    <ApplyModeBadge field={field} />
                  </span>
                }
              >
                <FieldInput
                  field={field}
                  value={values[field.path]}
                  placeholder={placeholderFor(effective?.[field.path])}
                  onChange={onChange}
                  disabled={disabled}
                />
              </FormField>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
