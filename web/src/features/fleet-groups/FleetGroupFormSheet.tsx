import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { FormField } from "@/ui/base/form-field";

export interface FleetGroupFormData {
  name: string;
  label: string;
  description: string;
}

export interface FleetGroupFormSheetProps {
  mode: "create" | "edit";
  data: FleetGroupFormData;
  onChange: (data: FleetGroupFormData) => void;
  onSubmit: () => void;
  onCancel: () => void;
  loading?: boolean;
  error?: string;
}

// Name is the immutable slug — editable only on create. We render it
// disabled in edit mode so operators see why they cannot change it.
export function FleetGroupFormSheet({
  mode,
  data,
  onChange,
  onSubmit,
  onCancel,
  loading,
  error,
}: FleetGroupFormSheetProps) {
  function update<K extends keyof FleetGroupFormData>(key: K, value: FleetGroupFormData[K]) {
    onChange({ ...data, [key]: value });
  }

  const isCreate = mode === "create";

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-title">{isCreate ? "New fleet group" : "Edit fleet group"}</h3>
        <p className="text-sm text-fg-muted mt-0.5">
          {isCreate
            ? "Pick a URL-safe slug and a human-readable label. The slug is permanent."
            : "Update the label and description. The slug stays locked for API/CLI stability."}
        </p>
      </div>

      <FormField label="Slug (immutable)" variant="uppercase" required>
        <Input
          value={data.name}
          onChange={(e) => update("name", e.target.value.toLowerCase())}
          placeholder="e.g. edge-eu"
          disabled={!isCreate || loading}
          className="font-mono"
        />
      </FormField>

      <FormField label="Label" variant="uppercase" required>
        <Input
          value={data.label}
          onChange={(e) => update("label", e.target.value)}
          placeholder="e.g. Edge Europe"
          disabled={loading}
        />
      </FormField>

      <FormField label="Description" variant="uppercase">
        <Input
          value={data.description}
          onChange={(e) => update("description", e.target.value)}
          placeholder="Optional — where, what, why"
          disabled={loading}
        />
      </FormField>

      {error && <div className="text-xs text-status-error">{error}</div>}

      <div className="flex gap-2 justify-end mt-2">
        <Button variant="ghost" onClick={onCancel} disabled={loading}>
          Cancel
        </Button>
        <Button
          onClick={onSubmit}
          disabled={loading || !data.label || (isCreate && !data.name)}
        >
          {loading ? "Saving…" : isCreate ? "Create group" : "Save changes"}
        </Button>
      </div>
    </div>
  );
}
