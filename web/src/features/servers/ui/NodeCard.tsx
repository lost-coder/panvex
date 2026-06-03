import { useTranslation } from "react-i18next";

import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";
import { StatusDot } from "@/ui/primitives/StatusDot";
import { ArrowUpCircle } from "lucide-react";
import { TransportBadge } from "@/features/servers/ui/TransportBadge";
import type { ModeKind, Severity } from "@/shared/api/types-pages/pages";

export interface NodeCardProps {
  name: string;
  status: Status;
  mode: ModeKind;
  healthyUpstreams: number;
  totalUpstreams: number;
  severity: Severity;
  cpu: number;
  mem: number;
  clients: number;
  region: string;
  alert?: boolean;
  /** When true, shows an update-available icon in the top-right corner. */
  updateAvailable?: boolean;
  /**
   * When true, render an "idle" chip — used by the direct-mode panel to
   * call out nodes that are currently carrying no client traffic.
   */
  idle?: boolean;
  onClick?: () => void;
  className?: string;
}

export function NodeCard({
  name,
  status,
  mode,
  healthyUpstreams,
  totalUpstreams,
  severity,
  cpu,
  mem,
  clients,
  region,
  alert,
  updateAvailable,
  idle,
  onClick,
  className,
}: Readonly<NodeCardProps>) {
  const { t } = useTranslation("servers");
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "relative flex flex-col gap-2 rounded-xs bg-bg-card p-3 text-left w-full",
        "border border-transparent hover:border-border-hi hover:bg-bg-card-hi transition-colors",
        alert && "border-l-[3px] border-l-status-error",
        className,
      )}
    >
      {updateAvailable && (
        <ArrowUpCircle
          className="absolute top-2 right-2 w-4 h-4 text-accent"
          aria-label={t("list.card.updateAvailable")}
        />
      )}
      <div className="flex items-center gap-2">
        <StatusDot status={status} size="md" />
        <span className="font-mono font-semibold text-sm text-fg flex-1 truncate">{name}</span>
        <span className="text-caption">{region}</span>
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        <TransportBadge
          mode={mode}
          healthy={healthyUpstreams}
          total={totalUpstreams}
          severity={severity}
        />
        {idle && (
          <span
            className={cn(
              "inline-flex items-center px-2 py-0.5 rounded-xs border font-mono text-xs",
              "bg-bg-card-hi text-fg-muted border-border",
            )}
          >
            {t("list.card.idle")}
          </span>
        )}
      </div>

      <div className="grid grid-cols-3 gap-2">
        <Metric value={`${cpu}%`} label={t("list.columns.cpu")} />
        <Metric value={`${mem}%`} label={t("list.columns.mem")} />
        <Metric value={String(clients)} label={t("list.columns.clients")} />
      </div>
    </button>
  );
}

function Metric({ value, label }: Readonly<{ value: string; label: string }>) {
  return (
    <div className="flex flex-col">
      <span className="text-xs font-mono font-medium text-fg leading-none">{value}</span>
      <span className="text-nano text-fg-muted uppercase tracking-wider mt-0.5">{label}</span>
    </div>
  );
}
