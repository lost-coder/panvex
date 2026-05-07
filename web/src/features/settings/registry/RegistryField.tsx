import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { Toggle } from "@/ui/base/toggle";
import { SettingsRow } from "@/ui/components/SettingsRow";
import type { SchemaEntry, ValuesEntry } from "./types";

export interface RegistryFieldProps {
  schema: SchemaEntry;
  values: ValuesEntry;
  onChange: (name: string, value: string) => void;
  error?: string;
}

// Source pill label: what's locking this field.
function sourceLabel(entry: ValuesEntry): string {
  if (entry.source === "env") {
    return entry.env_var ? `Set via ${entry.env_var}` : "Set via env";
  }
  if (entry.source === "config_file") {
    return "Set in config.toml";
  }
  return "Default";
}

// Stringify value for controlled inputs.
function toStr(v: unknown): string {
  if (v === null || v === undefined) return "";
  return String(v);
}

export function RegistryField({ schema, values, onChange, error }: Readonly<RegistryFieldProps>) {
  const { name, type, desc, values: enumValues } = schema;
  const { value, locked, pending_restart, pending_value } = values;

  const disabled = locked;
  const hasPendingChange =
    pending_restart === true && String(pending_value) !== String(value);

  // json type — no editable input; just a note.
  if (type === "json") {
    return (
      <SettingsRow label={name} description={desc}>
        <span className="text-xs text-fg-muted italic">
          Edit via the dedicated section below
        </span>
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
        ? "e.g. 30s, 5m, 1h"
        : type === "hostport"
          ? "e.g. 0.0.0.0:8080"
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
          {locked && (
            <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-mono font-semibold bg-fg-faint/20 text-fg-muted">
              {sourceLabel(values)}
            </span>
          )}
          {hasPendingChange && (
            <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-mono font-semibold bg-status-warn/15 text-status-warn">
              restart pending
            </span>
          )}
        </div>
        {error && (
          <span className="text-xs text-status-error">{error}</span>
        )}
      </div>
    </SettingsRow>
  );
}
