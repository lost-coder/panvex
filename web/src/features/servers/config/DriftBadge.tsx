// P5-T4: drift-status badge for the Config tab.
//
// Maps the agent config drift status (in_sync | drifted | unknown) to a
// shared @/ui Badge variant + an i18n label under servers.config.drift.*.
// Purely presentational — the status comes from AgentConfig.drift.status.

import { Badge, type BadgeProps } from "@/ui";
import { useTranslation } from "react-i18next";

export type DriftStatus = "in_sync" | "drifted" | "unknown";

const VARIANT: Record<DriftStatus, NonNullable<BadgeProps["variant"]>> = {
  drifted: "warn",
  in_sync: "ok",
  unknown: "default",
};

export interface DriftBadgeProps {
  status: DriftStatus;
}

export function DriftBadge({ status }: Readonly<DriftBadgeProps>) {
  const { t } = useTranslation("servers");
  return <Badge variant={VARIANT[status]}>{t(`config.drift.${status}`)}</Badge>;
}
