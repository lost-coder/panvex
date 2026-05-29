import { Lock, RefreshCw } from "lucide-react";
import { cn } from "@/ui/lib/cn";
import type { IndicatorTone } from "./indicators";

export interface IndicatorIconProps {
  icon: "lock" | "restart";
  tone: IndicatorTone;
  spinning?: boolean;
  className?: string;
}

// Presentational-only lucide glyph used by RegistryField and SettingsLegend.
// `aria-hidden` because the accessible label lives on the wrapping trigger.
export function IndicatorIcon({ icon, tone, spinning, className }: Readonly<IndicatorIconProps>) {
  const Glyph = icon === "lock" ? Lock : RefreshCw;
  return (
    <Glyph
      aria-hidden
      className={cn(
        "h-4 w-4",
        tone === "amber" ? "text-status-warn" : "text-fg-muted",
        spinning && "animate-spin [animation-duration:2.4s]",
        className,
      )}
    />
  );
}
