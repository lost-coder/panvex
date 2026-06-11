// R-Q-08: hero band + mobile page-header + actions strip extracted
// from ClientDetailPage.tsx. The layout twins (mobile PageHeader vs
// desktop HeroStrip) live together so a designer changing the action
// list updates one file instead of two parallel JSX sections.
//
// Plan 2h: status is the unified 7-state ClientStateBadge (same badge
// the clients list/filter use). Desktop puts it in the HeroStrip
// `prefix` slot (before the name); mobile puts it in the PageHeader
// `trailing` slot — mirroring ServerHero / ServerDetailPage.

import { useTranslation } from "react-i18next";
import { MoreVertical } from "lucide-react";

import {
  Button,
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  HeroStrip,
  PageHeader,
  type HeroMetaPill,
  type StatusTone,
} from "@/ui";

import { ClientStateBadge } from "./ClientsPageCells";
import type { ClientState } from "./ClientsPageCells";
import { expiresSuffix, expiresTone } from "./clientDetailHelpers";

export interface ClientDetailHeroProps {
  name: string;
  enabled: boolean;
  expirationRfc3339: string;
  fleetGroupIds: string[];
  state: ClientState;
  hasFailedDeployment: boolean;
  onEdit?: (() => void) | undefined;
  onDisable?: (() => void) | undefined;
  onRedeploy?: (() => void) | undefined;
  redeploying?: boolean | undefined;
  onDelete?: (() => void) | undefined;
  /**
   * Reset-quota Phase 2: shown only when there are ≥2 deployments
   * (single-deployment clients use the per-row affordance directly).
   * Disabled while a fan-out job is in flight to prevent a double-fire.
   */
  onResetQuotaEverywhere?: (() => void) | undefined;
  resetEverywherePending?: boolean | undefined;
}

export function ClientDetailHero({
  name,
  enabled,
  expirationRfc3339,
  fleetGroupIds,
  state,
  hasFailedDeployment,
  onEdit,
  onDisable,
  onRedeploy,
  redeploying,
  onDelete,
  onResetQuotaEverywhere,
  resetEverywherePending,
}: Readonly<ClientDetailHeroProps>) {
  const { t } = useTranslation("clients");

  // U-04: on desktop the full action list lives in the hero's `actions`
  // strip, but the mobile PageHeader only had room for Edit — leaving
  // Disable/Redeploy/Reset/Delete unreachable from a phone. Surface them
  // in a kebab menu so client lifecycle is manageable on mobile too.
  const hasMobileMenu =
    !!onDisable ||
    (hasFailedDeployment && !!onRedeploy) ||
    !!onResetQuotaEverywhere ||
    !!onDelete;
  const mobileMenu = hasMobileMenu ? (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          size="sm"
          variant="ghost"
          aria-label={t("detail.actions.more")}
          className="px-2"
        >
          <MoreVertical size={18} aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {onDisable && (
          <DropdownMenuItem onSelect={onDisable}>
            {enabled ? t("detail.actions.disable") : t("detail.actions.enable")}
          </DropdownMenuItem>
        )}
        {hasFailedDeployment && onRedeploy && (
          <DropdownMenuItem onSelect={onRedeploy} disabled={!!redeploying}>
            {redeploying ? t("detail.actions.redeploying") : t("detail.actions.redeploy")}
          </DropdownMenuItem>
        )}
        {onResetQuotaEverywhere && (
          <DropdownMenuItem onSelect={onResetQuotaEverywhere} disabled={!!resetEverywherePending}>
            {resetEverywherePending
              ? t("detail.quota.resetting")
              : t("detail.quota.resetEverywhereButton")}
          </DropdownMenuItem>
        )}
        {onDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem danger onSelect={onDelete}>
              {t("detail.actions.delete")}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  ) : null;

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
      {onResetQuotaEverywhere && (
        <Button
          size="sm"
          variant="outline"
          onClick={onResetQuotaEverywhere}
          disabled={resetEverywherePending}
        >
          {resetEverywherePending
            ? t("detail.quota.resetting")
            : t("detail.quota.resetEverywhereButton")}
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
      {/* Mobile — PageHeader carries name + expiry subtitle; the unified
          status badge sits in the trailing slot beside the Edit button. */}
      <div className="md:hidden">
        <PageHeader
          title={name}
          subtitle={t("detail.subtitle", { expires: expiresSuffix(expirationRfc3339) })}
          trailing={
            <div className="flex items-center gap-2">
              <ClientStateBadge state={state} />
              {onEdit && (
                <Button size="sm" onClick={onEdit}>
                  {t("detail.actions.edit")}
                </Button>
              )}
              {mobileMenu}
            </div>
          }
        />
      </div>

      {/* Desktop hero — full-bleed band, matches the Server detail style.
          The status badge takes the prefix slot (before the name). */}
      <HeroStrip
        className="hidden md:flex"
        prefix={<ClientStateBadge state={state} />}
        name={name}
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
