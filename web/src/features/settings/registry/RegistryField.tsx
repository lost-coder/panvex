import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { Toggle } from "@/ui/base/toggle";
import { SettingsRow } from "@/ui/components/SettingsRow";
import { Badge } from "@/ui/primitives/Badge";
import type { SchemaEntry, ValuesEntry } from "./types";

export interface RegistryFieldProps {
  schema: SchemaEntry;
  values: ValuesEntry;
  onChange: (name: string, value: string) => void;
  error?: string;
}

// Source pill label: what's locking this field.
function sourceLabel(entry: ValuesEntry, t: TFunction): string {
  if (entry.source === "env") {
    return entry.env_var
      ? t("registryField.sourceEnvNamed", { name: entry.env_var })
      : t("registryField.sourceEnv");
  }
  if (entry.source === "config_file") {
    return t("registryField.sourceConfigFile");
  }
  return t("registryField.sourceDefault");
}

// Stringify value for controlled inputs.
function toStr(v: unknown): string {
  if (v === null || v === undefined) return "";
  return String(v);
}

export function RegistryField({ schema, values, onChange, error }: Readonly<RegistryFieldProps>) {
  const { t } = useTranslation("settings");
  const { name, type, desc, values: enumValues } = schema;
  const { value, locked, pending_restart, pending_value, overridden_by_env } = values;

  const disabled = locked;
  const hasPendingChange =
    pending_restart === true && String(pending_value) !== String(value);

  // Apply tier — prefer the values entry, fall back to the schema entry.
  const apply = values.apply ?? schema.apply;
  const isConfigTier = apply === "config" || schema.class === "bootstrap";
  const isRestartTier = apply === "restart";

  const tierBadge = isRestartTier ? (
    <Badge variant="warn">{t("registryField.tierRestart")}</Badge>
  ) : isConfigTier ? (
    <Badge variant="default">{t("registryField.tierConfig")}</Badge>
  ) : null;

  const envOverrideBadge =
    overridden_by_env === true ? (
      <Badge variant="warn">
        {t("registryField.overriddenByEnv", { name: values.env_var ?? "" })}
      </Badge>
    ) : null;

  const configManagedHint =
    isConfigTier && locked ? (
      <span className="text-xs text-fg-muted italic">
        {t("registryField.configManagedHint")}
      </span>
    ) : null;

  // json type — no editable input; just a note (plus any tier/env badges).
  if (type === "json") {
    return (
      <SettingsRow label={name} description={desc}>
        <div className="flex flex-col items-end gap-1">
          {(tierBadge || envOverrideBadge) && (
            <div className="flex items-center gap-2">
              {tierBadge}
              {envOverrideBadge}
            </div>
          )}
          <span className="text-xs text-fg-muted italic">
            {t("registryField.jsonNotice")}
          </span>
          {configManagedHint}
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
        <Input
          className="w-32"
          type="number"
          value={toStr(value)}
          disabled={disabled}
          onChange={(e) => onChange(name, e.target.value)}
          aria-label={name}
        />
      );
    }

    if (type === "url") {
      return (
        <Input
          className="w-64"
          type="url"
          value={toStr(value)}
          disabled={disabled}
          onChange={(e) => onChange(name, e.target.value)}
          aria-label={name}
        />
      );
    }

    // duration, hostport, string — text input with placeholder hint.
    const placeholder =
      type === "duration"
        ? t("registryField.placeholderDuration")
        : type === "hostport"
          ? t("registryField.placeholderHostport")
          : undefined;

    return (
      <Input
        className="w-64"
        type="text"
        value={toStr(value)}
        disabled={disabled}
        placeholder={placeholder}
        onChange={(e) => onChange(name, e.target.value)}
        aria-label={name}
      />
    );
  }

  return (
    <SettingsRow label={name} description={desc}>
      <div className="flex flex-col items-end gap-1">
        <div className="flex items-center gap-2">
          {renderInput()}
          {tierBadge}
          {envOverrideBadge}
          {locked && (
            <Badge variant="default">{sourceLabel(values, t)}</Badge>
          )}
          {hasPendingChange && (
            <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-mono font-semibold bg-status-warn/15 text-status-warn">
              {t("registryField.pendingRestart")}
            </span>
          )}
        </div>
        {configManagedHint}
        {error && (
          <span className="text-xs text-status-error">{error}</span>
        )}
      </div>
    </SettingsRow>
  );
}
