import { useTranslation } from "react-i18next";
import { cn } from "@/ui/lib/cn";
import { UserAvatar } from "@/ui/primitives/UserAvatar";
import { ConnectionBadge } from "@/ui/primitives/ConnectionBadge";
import { TrafficCell } from "@/ui/primitives/TrafficCell";

export interface UserCardProps {
  name: string;
  online: boolean;
  connections: number;
  trafficUp: number;
  trafficDown: number;
  ips?: number;
  onClick?: () => void;
  className?: string;
}

export function UserCard({
  name,
  online,
  connections,
  trafficUp,
  trafficDown,
  ips,
  onClick,
  className,
}: Readonly<UserCardProps>) {
  const { t } = useTranslation("users");
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-3 w-full rounded-xs bg-bg-card p-3 text-left",
        "border border-transparent hover:border-border-hi hover:bg-bg-card-hi transition-colors",
        className,
      )}
    >
      <UserAvatar name={name} online={online} />
      <div className="flex-1 min-w-0 flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-mono font-medium text-fg truncate">{name}</span>
          <ConnectionBadge online={online} count={connections} />
        </div>
        <div className="flex items-center gap-3">
          <TrafficCell bytes={trafficDown} label="↓" />
          <TrafficCell bytes={trafficUp} label="↑" />
          {ips !== undefined && (
            <span className="text-[11px] font-mono text-fg-muted">
              {t("card.ips", { count: ips })}
            </span>
          )}
        </div>
      </div>
      <span className="text-fg-muted/40 text-sm shrink-0">›</span>
    </button>
  );
}
