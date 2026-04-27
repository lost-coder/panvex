import { cn } from "@/ui/lib/cn";
import { AlertItem, type AlertItemProps } from "@/ui/components/AlertItem";

export interface AlertStripProps {
  alerts: AlertItemProps[];
  className?: string;
}

export function AlertStrip({ alerts, className }: Readonly<AlertStripProps>) {
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      {alerts.map((alert) => (
        <AlertItem key={`${alert.severity}-${alert.message}`} {...alert} />
      ))}
    </div>
  );
}
