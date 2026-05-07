import { Settings } from "lucide-react";
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
  const label = labelFor(namespace);
  // Skip json-typed fields — they have dedicated sections.
  const renderable = fields.filter((f) => f.schema.type !== "json");

  return (
    <PageSection
      icon={Settings}
      title={label.title}
      description={label.desc || undefined}
    >
      {renderable.map((f) => (
        <RegistryField
          key={f.schema.name}
          schema={f.schema}
          values={f.values}
          onChange={onChange}
          error={f.error}
        />
      ))}
    </PageSection>
  );
}
