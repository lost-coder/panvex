// P4-T2: small ℹ glyph that surfaces a param's localized description
// (ParamCatalogEntry.en/.ru) in a Tooltip. Kept as its own component so
// ConfigTreeField's label row stays readable and the language-pick logic
// (ru when the active language is "ru", en otherwise) lives in one place.
import { useTranslation } from "react-i18next";

import { Tooltip } from "@/ui";

import type { ParamCatalogEntry } from "./paramCatalog";

export interface InfoTooltipProps {
  entry: Pick<ParamCatalogEntry, "en" | "ru">;
}

export function InfoTooltip({ entry }: Readonly<InfoTooltipProps>) {
  const { i18n } = useTranslation("servers");
  const description = i18n.language === "ru" ? (entry.ru || entry.en) : entry.en;
  return (
    <Tooltip content={description}>
      <span
        role="img"
        aria-label={description}
        className="inline-flex h-4 w-4 shrink-0 cursor-help items-center justify-center rounded-full text-nano font-semibold text-fg-muted ring-1 ring-inset ring-border-hi"
      >
        i
      </span>
    </Tooltip>
  );
}
