import type { TFunction } from "i18next";

export interface NamespaceLabel {
  title: string;
  desc: string;
}

// Known operational namespaces with localised titles + descriptions.
// Keep this list in sync with src/locales/{en,ru}/settings.json →
// "registryNamespaces.*".
export const KNOWN_NAMESPACES = [
  "http",
  "agents",
  "auth",
  "jobs",
  "observability",
  "storage",
] as const;

export type KnownNamespace = (typeof KNOWN_NAMESPACES)[number];

export function namespaceOf(name: string): string {
  const dot = name.indexOf(".");
  return dot >= 0 ? name.slice(0, dot) : name;
}

export function labelFor(namespace: string, t: TFunction): NamespaceLabel {
  if ((KNOWN_NAMESPACES as readonly string[]).includes(namespace)) {
    return {
      title: t(`registryNamespaces.${namespace}.title`),
      desc: t(`registryNamespaces.${namespace}.desc`),
    };
  }
  return { title: namespace, desc: "" };
}
