import { useTranslation } from "react-i18next";

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
  onChange: (data: Readonly<FleetGroupFormData>) => void;
  onSubmit: () => void;
  onCancel: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
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
}: Readonly<FleetGroupFormSheetProps>) {
  const { t } = useTranslation("fleet-groups");

  function update<K extends keyof FleetGroupFormData>(key: K, value: FleetGroupFormData[K]) {
    onChange({ ...data, [key]: value });
  }

  const isCreate = mode === "create";

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-title">
          {isCreate ? t("form.createTitle") : t("form.editTitle")}
        </h3>
        <p className="text-sm text-fg-muted mt-0.5">
          {isCreate ? t("form.createSubtitle") : t("form.editSubtitle")}
        </p>
      </div>

      <FormField label={t("form.slugLabel")} variant="uppercase" required>
        <Input
          value={data.name}
          onChange={(e) => update("name", e.target.value.toLowerCase())}
          placeholder={t("form.slugPlaceholder")}
          disabled={!isCreate || loading}
          className="font-mono"
        />
      </FormField>

      <FormField label={t("form.labelLabel")} variant="uppercase" required>
        <Input
          value={data.label}
          onChange={(e) => update("label", e.target.value)}
          placeholder={t("form.labelPlaceholder")}
          disabled={loading}
        />
      </FormField>

      <FormField label={t("form.descriptionLabel")} variant="uppercase">
        <Input
          value={data.description}
          onChange={(e) => update("description", e.target.value)}
          placeholder={t("form.descriptionPlaceholder")}
          disabled={loading}
        />
      </FormField>

      {error && <div className="text-xs text-status-error">{error}</div>}

      <div className="flex gap-2 justify-end mt-2">
        <Button variant="ghost" onClick={onCancel} disabled={loading}>
          {t("form.cancel")}
        </Button>
        <Button
          onClick={onSubmit}
          disabled={loading || !data.label || (isCreate && !data.name)}
        >
          {(() => {
            if (loading) return t("form.saving");
            if (isCreate) return t("form.create");
            return t("form.save");
          })()}
        </Button>
      </div>
    </div>
  );
}
