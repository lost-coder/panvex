// Task 9: read-only viewer for the node's LIVE config (Telemt's
// GET /v1/config, forwarded verbatim as `data.observed` — see
// configSectionsSchema in shared/api/schemas/config.ts). CONFIG_FIELDS
// (fieldRegistry.ts) only curates a subset of Telemt's config surface for
// editing; everything else the agent reports is real, operator-relevant
// state that the panel has no editor for. This component fills that gap:
// it flattens `observed` and renders every path NOT already covered by
// ConfigSectionEditor above, so nothing the node is actually running is
// hidden from the operator.
//
// Agent-scope only: fleet groups have no single node's observed config, so
// this is wired into ConfigTab (server-detail) and NOT GroupConfigSection.
import { useTranslation } from "react-i18next";

import { Badge } from "@/ui";
import type { ConfigSections } from "@/shared/api/schemas/config";

import { CONFIG_FIELDS } from "./fieldRegistry";
import { isProcessOwned } from "./guard";

export interface ObservedConfigViewerProps {
  observed: ConfigSections;
}

interface ObservedRow {
  path: string;
  value: unknown;
}

const MANAGED_PATHS: ReadonlySet<string> = new Set(CONFIG_FIELDS.map((f) => f.path));

/**
 * Flattens the observed sections object into "section.key"-style dotted
 * paths, in the order the API returned them (which mirrors Telemt's TOML
 * section/key order). Plain nested objects (e.g. a dc_overrides map keyed
 * by DC id) recurse one level further into "section.subkey.field"; arrays
 * and scalars are leaves.
 */
function flattenObserved(sections: Record<string, unknown>): ObservedRow[] {
  const rows: ObservedRow[] = [];
  const walk = (prefix: string, value: unknown) => {
    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      const entries = Object.entries(value as Record<string, unknown>);
      if (entries.length === 0) {
        rows.push({ path: prefix, value });
        return;
      }
      for (const [key, v] of entries) walk(`${prefix}.${key}`, v);
      return;
    }
    rows.push({ path: prefix, value });
  };
  for (const [section, value] of Object.entries(sections)) walk(section, value);
  return rows;
}

function formatValue(value: unknown): string {
  if (value === undefined || value === null) return "—";
  if (Array.isArray(value)) {
    return value.length === 0 ? "[]" : value.map(String).join(", ");
  }
  if (typeof value === "boolean") return value ? "true" : "false";
  return String(value);
}

export function ObservedConfigViewer({ observed }: Readonly<ObservedConfigViewerProps>) {
  const { t } = useTranslation("servers");
  const rows = flattenObserved(observed).filter((row) => !MANAGED_PATHS.has(row.path));

  return (
    <section className="flex flex-col gap-3 border-t border-divider pt-4">
      <div className="flex flex-col gap-1">
        <h3 className="text-xs font-medium uppercase tracking-wider text-fg-muted">
          {t("config.observed.title")}
        </h3>
        <p className="text-micro text-fg-muted">{t("config.observed.hint")}</p>
      </div>
      {rows.length === 0 ? (
        <p className="text-micro text-fg-muted">{t("config.observed.emptyState")}</p>
      ) : (
        <div className="flex flex-col text-xs">
          {rows.map((row) => (
            <div
              key={row.path}
              className="flex flex-wrap items-baseline gap-x-2 gap-y-1 border-b border-divider/60 py-1.5 last:border-b-0"
            >
              <span className="font-mono text-fg-muted">{row.path}</span>
              <span className="text-fg">{formatValue(row.value)}</span>
              {isProcessOwned(row.path) && (
                <Badge variant="warn">{t("config.observed.processOwnedNote")}</Badge>
              )}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
