import { cn } from "@/ui/lib/cn";

export interface StatusHeroProps {
  online: number;
  degraded: number;
  offline: number;
  className?: string;
}

export function StatusHero({ online, degraded, offline, className }: StatusHeroProps) {
  return (
    <div className={cn("grid grid-cols-3 gap-3", className)}>
      <HeroNumber value={online} label="Online" color="text-status-ok" />
      <HeroNumber value={degraded} label="Degraded" color="text-status-warn" />
      <HeroNumber value={offline} label="Offline" color="text-status-error" />
    </div>
  );
}

function HeroNumber({ value, label, color }: { value: number; label: string; color: string }) {
  return (
    <div className="flex flex-col items-center gap-1 rounded-xs bg-bg-card p-4">
      <span className={cn("text-3xl font-mono font-bold leading-none", color)}>{value}</span>
      <span className="text-caption uppercase tracking-wider">{label}</span>
    </div>
  );
}
