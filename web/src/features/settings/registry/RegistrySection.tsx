import { Settings } from "lucide-react";
import { useTranslation } from "react-i18next";
import { PageSection } from "@/ui/compositions/PageSection";
import { labelFor } from "./namespaceLabels";
import { RegistryField } from "./RegistryField";
import type { SchemaEntry, ValuesEntry } from "./types";

export interface RegistrySectionField {
  schema: SchemaEntry;
  values: ValuesEntry;
  error?: string;
}

export interface RegistrySectionProps {
  namespace: string;
  fields: RegistrySectionField[];
  onChange: (name: string, value: string) => void;
}

export function RegistrySection({ namespace, fields, onChange }: Readonly<RegistrySectionProps>) {
  const { t } = useTranslation("settings");
  const label = labelFor(namespace, t);
  // Skip json-typed fields — they have dedicated sections.
  const renderable = fields.filter((f) => f.schema.type !== "json");

  const descProps = label.desc ? { description: label.desc } : {};

  return (
    <PageSection
      icon={Settings}
      title={label.title}
      {...descProps}
    >
      {renderable.map((f) => {
        const errorProps = f.error ? { error: f.error } : {};
        return (
          <RegistryField
            key={f.schema.name}
            schema={f.schema}
            values={f.values}
            onChange={onChange}
            {...errorProps}
          />
        );
      })}
    </PageSection>
  );
}
