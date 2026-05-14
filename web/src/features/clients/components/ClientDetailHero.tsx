// R-Q-08: hero band + mobile page-header + actions strip extracted
// from ClientDetailPage.tsx. The layout twins (mobile PageHeader vs
// desktop HeroStrip) live together so a designer changing the action
// list updates one file instead of two parallel JSX sections.

import { useTranslation } from "react-i18next";

import {
  Button,
  HeroStrip,
  PageHeader,
  type HeroMetaPill,
  type StatusTone,
} from "@/ui";

import { expiresSuffix, expiresTone } from "./clientDetailHelpers";

function statusTone(
  status: "active" | "disabled" | "expired",
  enabled: boolean,
): StatusTone {
  if (status === "expired") return "error";
  if (enabled) return "ok";
  return "warn";
}

export interface ClientDetailHeroProps {
  name: string;
  enabled: boolean;
  expirationRfc3339: string;
  fleetGroupIds: string[];
  statusLabel: string;
  status: "active" | "disabled" | "expired";
  hasFailedDeployment: boolean;
  onEdit?: (() => void) | undefined;
  onDisable?: (() => void) | undefined;
  onRedeploy?: (() => void) | undefined;
  redeploying?: boolean | undefined;
  onDelete?: (() => void) | undefined;
}

export function ClientDetailHero({
  name,
  enabled,
  expirationRfc3339,
  fleetGroupIds,
  statusLabel,
  status,
  hasFailedDeployment,
  onEdit,
  onDisable,
  onRedeploy,
  redeploying,
  onDelete,
}: Readonly<ClientDetailHeroProps>) {
  const { t } = useTranslation("clients");
  const actions = (
    <>
      {onEdit && (
        <Button size="sm" variant="outline" onClick={onEdit}>
          {t("detail.actions.edit")}
        </Button>
      )}
      {onDisable && (
        <Button size="sm" variant="ghost" onClick={onDisable}>
          {enabled ? t("detail.actions.disable") : t("detail.actions.enable")}
        </Button>
      )}
      {hasFailedDeployment && onRedeploy && (
        <Button
          size="sm"
          variant="outline"
          onClick={onRedeploy}
          disabled={redeploying}
          className="text-status-warn border-status-warn/60 hover:text-status-warn"
          title={t("detail.actions.redeployTitle")}
        >
          {redeploying ? t("detail.actions.redeploying") : t("detail.actions.redeploy")}
        </Button>
      )}
      {onDelete && (
        <Button
          size="sm"
          variant="ghost"
          onClick={onDelete}
          className="text-status-error hover:text-status-error"
        >
          {t("detail.actions.delete")}
        </Button>
      )}
    </>
  );
  return (
    <>
      {/* Mobile — PageHeader carries name + status subtitle. */}
      <div className="md:hidden">
        <PageHeader
          title={name}
          subtitle={t("detail.subtitle", {
            status: statusLabel.toLowerCase(),
            expires: expiresSuffix(expirationRfc3339),
          })}
          trailing={
            onEdit ? (
              <Button size="sm" onClick={onEdit}>
                {t("detail.actions.edit")}
              </Button>
            ) : undefined
          }
        />
      </div>

      {/* Desktop hero — full-bleed band, matches the Server detail style. */}
      <HeroStrip
        className="hidden md:flex"
        name={name}
        status={{
          tone: statusTone(status, enabled),
          label: statusLabel,
        }}
        pills={[
          ...fleetGroupIds.map<HeroMetaPill>((g) => ({
            label: t("detail.hero.group"),
            value: g,
            mono: true,
          })),
          {
            label: t("detail.hero.expires"),
            value: expiresSuffix(expirationRfc3339),
            mono: true,
            tone: expiresTone(expirationRfc3339) as StatusTone,
          },
        ]}
        actions={actions}
      />
    </>
  );
}
