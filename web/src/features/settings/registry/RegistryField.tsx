import { useTranslation } from "react-i18next";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { Toggle } from "@/ui/base/toggle";
import { Tooltip } from "@/ui/base/tooltip";
import { SettingsRow } from "@/ui/components/SettingsRow";
import { cn } from "@/ui/lib/cn";
import type { SchemaEntry, ValuesEntry } from "./types";
import { BAR_SHADOW, resolveIndicator } from "./indicators";
import { IndicatorIcon } from "./IndicatorIcon";

export interface RegistryFieldProps {
  schema: SchemaEntry;
  values: ValuesEntry;
  onChange: (name: string, value: string) => void;
  error?: string;
  /** Suppress the accent bar + icon (used by the read-only System Info section). */
  hideIndicators?: boolean;
}

// Stringify value for controlled inputs.
function toStr(v: unknown): string {
  if (v === null || v === undefined) return "";
  return String(v);
}

export function RegistryField({ schema, values, onChange, error, hideIndicators }: Readonly<RegistryFieldProps>) {
  const { t } = useTranslation("settings");
  const { name, type, desc, values: enumValues } = schema;
  const { value, locked } = values;

  const disabled = locked;
  const indicator = resolveIndicator(schema, values);
  const showIndicator = !hideIndicators && indicator.icon !== null;

  const rowClass = showIndicator && indicator.bar ? BAR_SHADOW[indicator.bar] : undefined;

  const iconEl =
    showIndicator && indicator.icon ? (
      <Tooltip
        content={t(`registryField.tooltip.${indicator.tooltipKey}`, {
          name: values.env_var ?? "",
        })}
      >
        <button
          type="button"
          aria-label={t(`registryField.iconLabel.${indicator.icon}`)}
          className="inline-flex cursor-help items-center"
        >
          <IndicatorIcon icon={indicator.icon} tone={indicator.tone ?? "grey"} spinning={indicator.spinning} />
        </button>
      </Tooltip>
    ) : null;

  // json type — no editable input; just a note (plus any indicator icon).
  if (type === "json") {
    return (
      <SettingsRow label={name} description={desc} className={rowClass}>
        <div className="flex flex-col items-end gap-1">
          {iconEl}
          <span className="text-xs text-fg-muted italic">{t("registryField.jsonNotice")}</span>
        </div>
      </SettingsRow>
    );
  }

  function renderInput() {
    if (type === "bool") {
      return (
        <Toggle
          checked={value === true || value === "true"}
          onChange={(checked) => onChange(name, String(checked))}
          disabled={disabled}
        />
      );
    }
    if (type === "enum" && enumValues && enumValues.length > 0) {
      return (
        <Select
          className="w-48"
          options={enumValues.map((v) => ({ value: v, label: v }))}
          value={toStr(value)}
          onChange={(v) => onChange(name, v)}
          disabled={disabled}
        />
      );
    }
    if (type === "int") {
      return (
        <Input className="w-32" type="number" value={toStr(value)} disabled={disabled} onChange={(e) => onChange(name, e.target.value)} aria-label={name} />
      );
    }
    if (type === "url") {
      return (
        <Input className="w-64" type="url" value={toStr(value)} disabled={disabled} onChange={(e) => onChange(name, e.target.value)} aria-label={name} />
      );
    }
    const placeholder =
      type === "duration"
        ? t("registryField.placeholderDuration")
        : type === "hostport"
          ? t("registryField.placeholderHostport")
          : undefined;
    return (
      <Input className="w-64" type="text" value={toStr(value)} disabled={disabled} placeholder={placeholder} onChange={(e) => onChange(name, e.target.value)} aria-label={name} />
    );
  }

  return (
    <SettingsRow label={name} description={desc} className={cn(rowClass)}>
      <div className="flex flex-col items-end gap-1">
        <div className="flex items-center gap-2">
          {iconEl}
          {renderInput()}
        </div>
        {error && <span className="text-xs text-status-error">{error}</span>}
      </div>
    </SettingsRow>
  );
}
