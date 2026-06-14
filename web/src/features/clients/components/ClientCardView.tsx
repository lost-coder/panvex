// U-09: card layout for the clients list. The ViewMode toggle (shared
// with the Servers page) always rendered the table regardless of mode —
// the "cards" branch never existed. This mirrors ServerCardView so the
// toggle is honest: a responsive grid of client summary cards on desktop.

import { useTranslation } from "react-i18next";

import { type ClientListItem } from "@/ui";

import {
  ClientExpiryCell,
  ClientStateBadge,
  ClientTrafficCell,
  deriveClientState,
} from "./ClientsPageCells";

export interface ClientCardViewProps {
  clients: ClientListItem[];
  onClientClick?: ((id: string) => void) | undefined;
  nowMs: number;
}

export function ClientCardView({
  clients,
  onClientClick,
  nowMs,
}: Readonly<ClientCardViewProps>) {
  const { t } = useTranslation("clients");
  const nowSec = Math.floor(nowMs / 1000);
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {clients.map((c) => {
        const state = deriveClientState(c, nowMs);
        return (
          <button
            key={c.id}
            type="button"
            onClick={() => onClientClick?.(c.id)}
            className="flex flex-col gap-3 text-left rounded-xl bg-bg-card border border-border p-4 shadow-sm transition-colors hover:bg-bg-hover hover:border-border-hi focus-visible:outline-2 focus-visible:outline-accent"
          >
            <div className="flex items-center gap-2 min-w-0">
              <ClientStateBadge state={state} />
              <span className="font-medium text-fg truncate">{c.name}</span>
            </div>
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-micro">
              <div className="flex flex-col">
                <dt className="text-fg-muted uppercase tracking-wide">{t("table.usage")}</dt>
                <dd className="font-mono text-fg tabular-nums">
                  {c.activeTcpConns} {t("table.connsSuffix")} · {c.uniqueIpsUsed} {t("table.ipsSuffix")}
                </dd>
              </div>
              <div className="flex flex-col">
                <dt className="text-fg-muted uppercase tracking-wide">{t("table.traffic")}</dt>
                <dd className="font-mono text-fg tabular-nums">
                  <ClientTrafficCell used={c.trafficUsedBytes} quota={c.dataQuotaBytes} nodes={c.assignedNodesCount} />
                </dd>
              </div>
              <div className="flex flex-col">
                <dt className="text-fg-muted uppercase tracking-wide">{t("table.expires")}</dt>
                <dd className="font-mono text-fg tabular-nums">
                  <ClientExpiryCell rfc={c.expirationRfc3339} nowSec={nowSec} t={t} />
                </dd>
              </div>
              <div className="flex flex-col">
                <dt className="text-fg-muted uppercase tracking-wide">{t("table.nodes")}</dt>
                <dd className="font-mono text-fg tabular-nums">{c.assignedNodesCount}</dd>
              </div>
            </dl>
          </button>
        );
      })}
    </div>
  );
}
