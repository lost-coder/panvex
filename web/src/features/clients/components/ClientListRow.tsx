// R-Q-08: mobile compact row extracted from ClientsPage.tsx. Same
// shape as the original inline component — only the import paths change
// for the host page.

import { useTranslation } from "react-i18next";

import { formatBytes, type ClientListItem } from "@/ui";

import { ClientStateBadge, deriveClientState } from "./ClientsPageCells";

export interface ClientListRowProps {
  client: ClientListItem;
  onClick?: () => void;
  selectable?: boolean;
  selected?: boolean;
  onToggleSelect?: (id: string) => void;
  nowMs: number;
}

export function ClientListRow({
  client,
  onClick,
  selectable,
  selected,
  onToggleSelect,
  nowMs,
}: Readonly<ClientListRowProps>) {
  const { t } = useTranslation("clients");
  const state = deriveClientState(client, nowMs);
  return (
    <div
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick?.();
        }
      }}
      role="button"
      tabIndex={0}
      className="flex items-center gap-3 px-4 py-3 border-b border-divider hover:bg-bg-hover transition-colors cursor-pointer"
    >
      {selectable && (
        <input
          type="checkbox"
          aria-label={t("table.selectOne", { name: client.name })}
          checked={!!selected}
          onChange={() => onToggleSelect?.(client.id)}
          onClick={(e) => e.stopPropagation()}
          className="accent-accent size-4 cursor-pointer"
        />
      )}
      <ClientStateBadge state={state} />
      <div className="flex flex-col min-w-0 flex-1">
        <span className="font-medium text-fg truncate">{client.name}</span>
        <span className="text-micro font-mono text-fg-muted tabular-nums">
          {client.activeTcpConns} {t("table.connsSuffix")} · {formatBytes(client.trafficUsedBytes)}
        </span>
      </div>
    </div>
  );
}
