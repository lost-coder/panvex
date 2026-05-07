// Phase 3 — Task 7: data hook orchestrating schema + values + draft state + save.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { apiClient } from "@/shared/api/api";
import type { SchemaEntry, ValuesEntry } from "./types";

const SCHEMA_KEY = ["settings", "schema"] as const;
const VALUES_KEY = ["settings", "values"] as const;
const RESTART_KEY = ["settings", "restart-status"] as const;

export interface UseSettingsRegistryResult {
  schema: SchemaEntry[];
  values: Record<string, ValuesEntry>;
  bootstrapNames: Set<string>;
  pendingRestart: string[];
  draft: Record<string, string>;
  isDirty: boolean;
  errors: Record<string, string>;
  isLoading: boolean;
  isSaving: boolean;
  isRestartInFlight: boolean;
  setDraft(name: string, value: string): void;
  resetDraft(): void;
  save(): Promise<void>;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  restart(): Promise<any>;
}

export function useSettingsRegistry(): UseSettingsRegistryResult {
  const qc = useQueryClient();

  const schemaQ = useQuery({
    queryKey: SCHEMA_KEY,
    queryFn: () => apiClient.getSettingsSchema(),
    staleTime: 5 * 60 * 1000,
  });

  const valuesQ = useQuery({
    queryKey: VALUES_KEY,
    queryFn: () => apiClient.getSettingsValues(),
  });

  const restartQ = useQuery({
    queryKey: RESTART_KEY,
    queryFn: () => apiClient.getRestartStatus(),
  });

  const [draft, setDraftState] = useState<Record<string, string>>({});

  const schema = schemaQ.data ?? [];

  // Merge bootstrap + operational into a single flat lookup for rendering.
  const values: Record<string, ValuesEntry> = useMemo(() => {
    const out: Record<string, ValuesEntry> = {};
    if (valuesQ.data) {
      for (const [k, v] of Object.entries(valuesQ.data.bootstrap)) out[k] = v;
      for (const [k, v] of Object.entries(valuesQ.data.operational)) out[k] = v;
    }
    return out;
  }, [valuesQ.data]);

  const bootstrapNames = useMemo(
    () => new Set(Object.keys(valuesQ.data?.bootstrap ?? {})),
    [valuesQ.data],
  );

  const errors = useMemo(() => validateDraft(schema, draft), [schema, draft]);
  const isDirty = Object.keys(draft).length > 0;

  const saveMut = useMutation({
    mutationFn: () => apiClient.putSettingsValues(coerceForSave(schema, draft)),
    onSuccess: () => {
      setDraftState({});
      qc.invalidateQueries({ queryKey: VALUES_KEY });
      qc.invalidateQueries({ queryKey: RESTART_KEY });
    },
  });

  const restartMut = useMutation({
    mutationFn: () => apiClient.restartPanel(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: VALUES_KEY });
      qc.invalidateQueries({ queryKey: RESTART_KEY });
    },
  });

  return {
    schema,
    values,
    bootstrapNames,
    pendingRestart: restartQ.data?.fields ?? [],
    draft,
    isDirty,
    errors,
    isLoading: schemaQ.isLoading || valuesQ.isLoading,
    isSaving: saveMut.isPending,
    isRestartInFlight: restartMut.isPending,
    setDraft: (name: string, next: string) =>
      setDraftState((d) => ({ ...d, [name]: next })),
    resetDraft: () => setDraftState({}),
    save: () => saveMut.mutateAsync(),
    restart: () => restartMut.mutateAsync(),
  };
}

function validateDraft(
  schema: SchemaEntry[],
  draft: Record<string, string>,
): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const [name, raw] of Object.entries(draft)) {
    const f = schema.find((s) => s.name === name);
    if (!f) continue;
    if (f.type === "int") {
      const n = Number(raw);
      if (!Number.isFinite(n) || !Number.isInteger(n)) {
        errors[name] = "must be an integer";
        continue;
      }
      if (f.min !== undefined && n < Number(f.min)) errors[name] = `min ${f.min}`;
      if (f.max !== undefined && n > Number(f.max)) errors[name] = `max ${f.max}`;
    }
    if (f.type === "duration") {
      if (!/^\d+(\.\d+)?(ns|us|µs|ms|s|m|h)$/.test(raw)) {
        errors[name] = "expected duration like 30s, 5m, 1h";
      }
    }
    if (f.type === "enum" && f.values && !f.values.includes(raw)) {
      errors[name] = `must be one of: ${f.values.join(", ")}`;
    }
  }
  return errors;
}

function coerceForSave(
  schema: SchemaEntry[],
  draft: Record<string, string>,
): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {};
  for (const [name, raw] of Object.entries(draft)) {
    const f = schema.find((s) => s.name === name);
    if (!f) continue;
    if (f.type === "int") {
      out[name] = Number(raw);
      continue;
    }
    if (f.type === "bool") {
      out[name] = raw === "true";
      continue;
    }
    out[name] = raw;
  }
  return out;
}
